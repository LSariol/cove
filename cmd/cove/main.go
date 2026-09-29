package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/cli"
	"github.com/LSariol/Cove/internal/config"
	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/server"
	"github.com/LSariol/Cove/internal/vault"
)

// version is set at build time:
//
//	go build -ldflags "-X main.version=v1.0.0" ./cmd/cove
var version = "dev"

func main() {
	args := os.Args[1:]

	mode := ""
	if len(args) > 0 {
		mode = args[0]
	}

	switch mode {
	case "":
		runServer(true)
	case "serve":
		runServer(false)
	case "shell":
		runShell()
	case "migrate":
		runMigrate(loadConfig(), args[1:])
	case "version":
		fmt.Println(buildVersion())
	case "-h", "--help":
		printUsage(os.Stdout)
	default:
		os.Exit(runCommand(args))
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  cove                    Run the API server with the interactive CLI on stdin
  cove serve              Run the API server only
  cove shell              Open the interactive CLI (e.g. docker exec -it cove /cove shell)
  cove <command> [args]   Run one CLI command and exit, e.g. cove get MYAPP_KEY
  cove migrate [status|up]
                          Show or apply database migrations
  cove version            Print the version

Run "cove help" to list the CLI commands.
`)
}

// runServer runs the API server. With withShell, the interactive CLI also runs
// on stdin (plain `cove`); `cove serve` runs the server alone.
func runServer(withShell bool) {
	cfg := loadConfig()

	cfg, err := config.Ensure(cfg)
	if err != nil {
		fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		fatal(err)
	}
	for _, warning := range cfg.Warnings() {
		log.Printf("warning: %s", warning)
	}

	log.Printf("Cove %s", buildVersion())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations only run when the migrator connection is configured.
	if cfg.MigrateDatabaseURL != "" {
		if err := database.Migrate(ctx, cfg.MigrateDatabaseURL); err != nil {
			fatal(err)
		}
	}

	db := connect(ctx, cfg)
	v := vault.New(db, encryption.NewCipher(cfg.EncryptionKey))
	gate := bootstrap.NewGate(cfg.MarkerDir, mustBootstrapAllowed(cfg))

	srv := server.New(v, gate, db, server.Options{
		ClientSecret: cfg.ClientSecret,
		Port:         cfg.Port,
		Version:      buildVersion(),
	})

	if days, _ := cfg.RetentionDays(); days > 0 { // validated above
		go pruneReadEventsDaily(ctx, db, days)
	}

	if withShell {
		// When stdin closes (no terminal attached) the CLI simply returns and
		// the API keeps serving; `exit`, Ctrl+C and `docker stop` all cancel
		// ctx, which stops the server gracefully.
		shell := cli.New(v, gate, cli.Options{Embedded: true, Env: cfg.Env, Version: buildVersion(), DB: db})
		go shell.Run(ctx, stop)
	}

	err = srv.Run(ctx)
	db.Close()
	if err != nil {
		fatal(err)
	}
	log.Println("Cove stopped.")
}

// pruneReadEventsDaily removes read events older than days, now and then every
// 24 hours, until ctx is cancelled (COVE_EVENT_LOG_RETENTION_DAYS).
func pruneReadEventsDaily(ctx context.Context, db *database.Database, days int) {
	for {
		removed, err := db.PruneReadEvents(ctx, days)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("event log retention: %v", err)
		case removed > 0:
			log.Printf("event log retention: removed %d read events older than %d days", removed, days)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// runShell runs the interactive CLI on its own, next to a running server.
func runShell() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, shell := openClient(ctx)
	defer db.Close()

	done := make(chan struct{})
	go func() {
		shell.Run(ctx, stop)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// runCommand runs one CLI command, e.g. `cove get MYAPP_KEY`, and returns the
// process exit status: 0 on success, 1 on failure.
func runCommand(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, shell := openClient(ctx)
	defer db.Close()

	if err := shell.Exec(ctx, args); err != nil {
		cli.Report(err)
		return 1
	}
	return 0
}

// openClient connects the shell and one-shot commands to the vault. They use
// the same .env as the server but never generate secrets or run migrations:
// that's the server's job.
func openClient(ctx context.Context) (*database.Database, *cli.CLI) {
	cfg := loadConfig()

	if cfg.DatabaseURL == "" {
		fatal(errors.New("COVE_DATABASE_URL is not set"))
	}
	if cfg.EncryptionKey == "" {
		fatal(errors.New("VAULT_ENCRYPTION_KEY is not set. Start the Cove server once so it generates one"))
	}

	db := connect(ctx, cfg)
	v := vault.New(db, encryption.NewCipher(cfg.EncryptionKey))
	return db, cli.New(v, bootstrap.NewGate(cfg.MarkerDir, mustBootstrapAllowed(cfg)), cli.Options{Env: cfg.Env, Version: buildVersion(), DB: db})
}

// connect opens the database and confirms its schema matches this build.
func connect(ctx context.Context, cfg config.Config) *database.Database {
	db := database.New(cfg.DatabaseURL)

	if err := db.Connect(ctx); err != nil {
		fatal(err)
	}

	if err := db.CheckSchemaVersion(ctx); err != nil {
		db.Close()
		fatal(err)
	}
	return db
}

// mustBootstrapAllowed returns the networks allowed to bootstrap, or exits if
// COVE_BOOTSTRAP_ALLOWED_CIDRS can't be parsed.
func mustBootstrapAllowed(cfg config.Config) []netip.Prefix {
	allowed, err := cfg.BootstrapAllowed()
	if err != nil {
		fatal(err)
	}
	return allowed
}

func loadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	return cfg
}

// buildVersion returns version, or for an unstamped local build, "dev" plus the
// git commit it was built from when Go recorded one.
func buildVersion() string {
	if version != "dev" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				return "dev-" + setting.Value[:7]
			}
		}
	}
	return version
}

// runMigrate handles `cove migrate [status|up]` using COVE_MIGRATE_DATABASE_URL.
func runMigrate(cfg config.Config, args []string) {
	if cfg.MigrateDatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "cove migrate: COVE_MIGRATE_DATABASE_URL is not set. Set it to the cove_migrator connection string.")
		os.Exit(1)
	}

	command := "status"
	if len(args) > 0 {
		command = args[0]
	}

	ctx := context.Background()
	var err error

	switch command {
	case "status":
		err = database.PrintMigrationStatus(ctx, cfg.MigrateDatabaseURL, os.Stdout)
	case "up":
		err = database.Migrate(ctx, cfg.MigrateDatabaseURL)
	default:
		fmt.Fprintf(os.Stderr, "cove migrate: unknown command %q. Use \"status\" or \"up\".\n", command)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "cove migrate: %v\n", err)
		os.Exit(1)
	}
}

// fatal prints a startup error and exits. A clear message is more useful here
// than a panic's stack trace, especially in docker logs.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "cove: %v\n", err)
	os.Exit(1)
}
