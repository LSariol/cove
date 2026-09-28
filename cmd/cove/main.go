package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LSariol/Cove/internal/cli"
	"github.com/LSariol/Cove/internal/config"
	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/server"
)

func main() {

	if err := config.Load(); err != nil {
		panic(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		runMigrate(os.Args[2:])
		return
	}

	if err := config.Ensure(); err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations only run when the migrator connection is configured.
	if migrateURL := os.Getenv("COVE_MIGRATE_DATABASE_URL"); migrateURL != "" {
		if err := database.Migrate(ctx, migrateURL); err != nil {
			panic(fmt.Sprintf("Migrate DB: %v", err))
		}
	}

	db := database.NewDB()

	err := db.Connect(ctx)
	if err != nil {
		panic(fmt.Sprintf("Connect DB: %q", err))
	}

	if err := db.CheckSchemaVersion(ctx); err != nil {
		panic(fmt.Sprintf("Check DB schema: %v", err))
	}

	srv := server.NewServer(db)
	cli := cli.NewCLI(db)

	go srv.Start()

	cli.StartCLI(ctx)

	<-ctx.Done()
	log.Println("Shutting Down...")
}

// runMigrate handles `cove migrate [status|up]` using COVE_MIGRATE_DATABASE_URL.
func runMigrate(args []string) {
	migrateURL := os.Getenv("COVE_MIGRATE_DATABASE_URL")
	if migrateURL == "" {
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
		err = database.PrintMigrationStatus(ctx, migrateURL, os.Stdout)
	case "up":
		err = database.Migrate(ctx, migrateURL)
	default:
		fmt.Fprintf(os.Stderr, "unknown migrate command %q (use: status, up)\n", command)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
