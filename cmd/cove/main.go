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

	if err := config.Ensure(); err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db := database.NewDB()

	err := db.Connect(ctx)
	if err != nil {
		panic(fmt.Sprintf("Connect DB: %q", err))
	}

	srv := server.NewServer(db)
	cli := cli.NewCLI(db)

	go srv.Start()

	cli.StartCLI(ctx)

	<-ctx.Done()
	log.Println("Shutting Down...")
}
