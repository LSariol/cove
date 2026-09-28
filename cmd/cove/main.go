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

	if err := config.Load(); err != nil {
		panic(err)
	}

	cfg := config.FromEnv()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate(cfg, os.Args[2:])
		return
	}

	if err := config.Ensure(cfg); err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations only run when the migrator connection is configured.
	if cfg.MigrateDatabaseURL != "" {
		if err := database.Migrate(ctx, cfg.MigrateDatabaseURL); err != nil {
			panic(fmt.Sprintf("Migrate DB: %v", err))
		}
	}

	db := database.New(cfg.DatabaseURL)

	err := db.Connect(ctx)
	if err != nil {
		panic(fmt.Sprintf("Connect DB: %q", err))
	}

	if err := db.CheckSchemaVersion(ctx); err != nil {
		panic(fmt.Sprintf("Check DB schema: %v", err))
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
