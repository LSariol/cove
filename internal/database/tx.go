package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Store is everything the vault needs from the database. *Database implements
// it; vaulttest.Store is an in-memory version for tests.
type Store interface {
	InsertSecret(ctx context.Context, key string, encryptedValue string) (Secret, error)
	ReadSecret(ctx context.Context, key string) (Secret, error)
	GetSecret(ctx context.Context, key string) (Secret, error)
	GetSecretForUpdate(ctx context.Context, key string) (Secret, error)
	ListSecrets(ctx context.Context) ([]Secret, error)
	UpdateSecretValue(ctx context.Context, key string, encryptedValue string) (Secret, error)
	DeleteSecret(ctx context.Context, key string) (Secret, error)
	RenameSecret(ctx context.Context, oldKey string, newKey string) (Secret, error)
	LogEvent(ctx context.Context, logInfo EventLogInput) error
	ListEvents(ctx context.Context, key string, limit int) ([]Event, error)
	LastEvent(ctx context.Context, key string, kind EventKind) (Event, bool, error)
	ValueVersions(ctx context.Context, key string) (map[int]string, error)

	// WithinTx runs fn in a transaction: fn's changes are committed together
	// if it returns nil, and rolled back if it returns an error.
	WithinTx(ctx context.Context, fn func(tx Store) error) error
}

// querier is what the store's queries run on: the pool, or a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// conn returns what queries should run on: the transaction when inside
// WithinTx, the pool otherwise.
func (d *Database) conn() querier {
	if d.tx != nil {
		return d.tx
	}
	return d.Pool
}

// WithinTx runs fn in a transaction. Inside fn, the Store passed in runs every
// query in that transaction. Calling WithinTx again inside fn reuses it.
func (d *Database) WithinTx(ctx context.Context, fn func(tx Store) error) error {
	if d.tx != nil {
		return fn(d)
	}

	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		return fn(&Database{Pool: d.Pool, ConnString: d.ConnString, tx: tx})
	})
}
