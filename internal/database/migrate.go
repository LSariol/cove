package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// migrationsTable is where goose records applied migrations. It lives in the
// cove schema so everything Cove owns stays in one place.
const migrationsTable = "cove.goose_db_version"

// Migrate applies all pending migrations. connString must log in as the
// migrator role (cove_migrator), which acts as cove_owner.
func Migrate(ctx context.Context, connString string) error {
	provider, db, err := newMigrationProvider(ctx, connString)
	if err != nil {
		return err
	}
	defer db.Close()

	results, err := provider.Up(ctx)
	for _, r := range results {
		log.Printf("migration: %s", r)
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	if len(results) == 0 {
		log.Println("migration: database is up to date")
	}
	return nil
}

// PrintMigrationStatus writes each migration and whether it has been applied.
func PrintMigrationStatus(ctx context.Context, connString string, w io.Writer) error {
	provider, db, err := newMigrationProvider(ctx, connString)
	if err != nil {
		return err
	}
	defer db.Close()

	statuses, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}

	for _, s := range statuses {
		appliedAt := "-"
		if !s.AppliedAt.IsZero() {
			appliedAt = s.AppliedAt.Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%-8s %-19s %s\n", s.State, appliedAt, s.Source.Path)
	}
	return nil
}

// CheckSchemaVersion returns an error if the database hasn't had every embedded
// migration applied. It runs with the app role, so Cove refuses to start against
// an old schema instead of failing on its first query.
func (d *Database) CheckSchemaVersion(ctx context.Context) error {
	want, err := latestMigrationVersion()
	if err != nil {
		return err
	}

	var have int64
	const query = `SELECT COALESCE(MAX(version_id), 0) FROM cove.goose_db_version WHERE is_applied`
	if err := d.Pool.QueryRow(ctx, query).Scan(&have); err != nil {
		return fmt.Errorf("read schema version (has the database been migrated? set COVE_MIGRATE_DATABASE_URL): %w", err)
	}

	if have < want {
		return fmt.Errorf("database schema is at version %d but this build needs %d: set COVE_MIGRATE_DATABASE_URL to apply migrations", have, want)
	}
	return nil
}

func newMigrationProvider(ctx context.Context, connString string) (*goose.Provider, *sql.DB, error) {
	db, err := sql.Open("pgx", connString)
	if err != nil {
		return nil, nil, fmt.Errorf("open migration connection: %w", err)
	}

	// goose creates its version table before running any migration, so the
	// schema that holds it has to exist first.
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS cove"); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create cove schema: %w", err)
	}

	fsys, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	// Prevents two Cove instances from migrating at the same time.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithTableName(migrationsTable),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create migration provider: %w", err)
	}

	return provider, db, nil
}

func latestMigrationVersion() (int64, error) {
	names, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		return 0, err
	}

	var latest int64
	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			return 0, fmt.Errorf("migration %q: %w", name, err)
		}
		latest = max(latest, v)
	}
	return latest, nil
}
