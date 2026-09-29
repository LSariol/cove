// Package database manages the connection pool, migrations, and SQL queries for
// the Cove database. It stores and returns encrypted values only; encryption and
// the rules around reads and writes live in the vault package.
package database

import (
	"context"
	"fmt"
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

// Connect opens a pgxpool connection and validates it with Ping. Errors name
// COVE_DATABASE_URL, since that's what needs fixing.
func (d *Database) Connect(ctx context.Context) error {

	pool, err := pgxpool.New(ctx, d.ConnString)
	if err != nil {
		return fmt.Errorf("COVE_DATABASE_URL is not a valid connection string: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return fmt.Errorf("can't reach the database at COVE_DATABASE_URL: %w", err)
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
