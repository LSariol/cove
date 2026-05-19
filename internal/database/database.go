// Package database manages the connection pool and CRUD operations for the Cove Database.
package database

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	Pool       *pgxpool.Pool
	ConnString string
}

// Creates a new database object
func NewDB() *Database {

	return &Database{
		ConnString: os.Getenv("COVE_DATABASE_URL"),
	}
}

// Connect opens a pgxpool connection using environment variables and validlates its a successful connection with Ping
func (d *Database) Connect(ctx context.Context) error {

	pool, err := pgxpool.New(ctx, d.ConnString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return fmt.Errorf("ping database: %w", err)
	}

	d.Pool = pool
	return nil
}

// Close closes a pgxpool connection.
func (d *Database) Close() {
	if d.Pool != nil {
		d.Pool.Close()
	}
}
