package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/cli"
	"github.com/LSariol/Cove/internal/config"
	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/server"
	"github.com/LSariol/Cove/internal/vault"
)

func main() {

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate(cfg, os.Args[2:])
		return
	}

	cfg, err = config.Ensure(cfg)
	if err != nil {
		fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		fatal(err)
	}
	for _, warning := range cfg.Warnings() {
		log.Printf("warning: %s", warning)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations only run when the migrator connection is configured.
	if cfg.MigrateDatabaseURL != "" {
		if err := database.Migrate(ctx, cfg.MigrateDatabaseURL); err != nil {
			fatal(fmt.Errorf("migrate database: %w", err))
		}
	}

	db := database.New(cfg.DatabaseURL)

	err = db.Connect(ctx)
	if err != nil {
		fatal(fmt.Errorf("connect to database: %w", err))
	}

	if err := db.CheckSchemaVersion(ctx); err != nil {
		fatal(fmt.Errorf("check database schema: %w", err))
	}

	v := vault.New(db, encryption.NewCipher(cfg.EncryptionKey))
	marker := bootstrap.NewMarker(cfg.MarkerDir)

	srv := server.New(v, marker, cfg.ClientSecret, cfg.Port)
	cli := cli.New(v, marker)

	go srv.Start()

	cli.StartCLI(ctx)

	<-ctx.Done()
	log.Println("Shutting Down...")
}

// runMigrate handles `cove migrate [status|up]` using COVE_MIGRATE_DATABASE_URL.
func runMigrate(cfg config.Config, args []string) {
	if cfg.MigrateDatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "COVE_MIGRATE_DATABASE_URL is not set")
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
		fmt.Fprintf(os.Stderr, "unknown migrate command %q (use: status, up)\n", command)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fatal prints a startup error and exits. A clear message is more useful here
// than a panic's stack trace, especially in docker logs.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "cove: %v\n", err)
	os.Exit(1)
}
