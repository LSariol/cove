// Package database manages the connection pool, migrations, and SQL queries for
// the Cove database. It stores and returns encrypted values only; encryption and
// the rules around reads and writes live in the vault package.
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

// New returns a Database for connString. Call Connect before using it.
func New(connString string) *Database {

	return &Database{
		ConnString: connString,
	}
}

// Connect opens a pgxpool connection and validates it with Ping
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

// Ping checks that the database is reachable, giving up after 2 seconds.
func (d *Database) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.Pool.Ping(ctx)
}

// Close closes a pgxpool connection.
func (d *Database) Close() {
	if d.Pool != nil {
		d.Pool.Close()
	}
}
