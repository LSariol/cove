package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrNotFound is returned when no secret has the requested key.
	ErrNotFound = errors.New("secret not found")

	// ErrAlreadyExists is returned when creating a secret whose key is taken.
	ErrAlreadyExists = errors.New("a secret with this key already exists")
)

// uniqueViolation is Postgres's error code for a duplicate unique key.
const uniqueViolation = "23505"

// classify replaces driver errors callers need to act on with ErrNotFound or
// ErrAlreadyExists, and returns every other error unchanged.
func classify(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return ErrAlreadyExists
	}

	return err
}

// secretColumns is the column list scanned by scanSecret, in order.
const secretColumns = `id, key, encrypted_value, version, read_count, created_at, updated_at`

// InsertSecret stores a new secret and returns the created row.
func (d *Database) InsertSecret(ctx context.Context, key string, encryptedValue string) (Secret, error) {
	const query = `
	INSERT INTO cove.secrets (key, encrypted_value)
	VALUES ($1, $2)
	RETURNING ` + secretColumns

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, key, encryptedValue))
	if err != nil {
		return s, fmt.Errorf("insert secret %q: %w", key, classify(err))
	}
	return s, nil
}

// ReadSecret returns a secret and counts the read by incrementing read_count.
func (d *Database) ReadSecret(ctx context.Context, key string) (Secret, error) {
	const query = `
	UPDATE cove.secrets
	SET read_count = read_count + 1
	WHERE key = $1
	RETURNING ` + secretColumns

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, key))
	if err != nil {
		return s, fmt.Errorf("read secret %q: %w", key, classify(err))
	}
	return s, nil
}

// GetSecret returns a secret without counting it as a read.
func (d *Database) GetSecret(ctx context.Context, key string) (Secret, error) {
	const query = `
	SELECT ` + secretColumns + `
	FROM cove.secrets
	WHERE key = $1`

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, key))
	if err != nil {
		return s, fmt.Errorf("get secret %q: %w", key, classify(err))
	}
	return s, nil
}

// ListSecrets returns every secret ordered by key, without encrypted values.
func (d *Database) ListSecrets(ctx context.Context) ([]Secret, error) {
	const query = `
	SELECT id, key, version, read_count, created_at, updated_at
	FROM cove.secrets
	ORDER BY key ASC`

	rows, err := d.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query secrets: %w", err)
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		var s Secret
		if err := rows.Scan(
			&s.ID,
			&s.Key,
			&s.Version,
			&s.ReadCount,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan secret: %w", err)
		}
		secrets = append(secrets, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return secrets, nil
}

// UpdateSecretValue replaces a secret's value, increments its version, and
// returns the updated row. updated_at is set by the set_updated_at trigger.
func (d *Database) UpdateSecretValue(ctx context.Context, key string, encryptedValue string) (Secret, error) {
	const query = `
	UPDATE cove.secrets
	SET
		encrypted_value = $2,
		version = version + 1
	WHERE key = $1
	RETURNING ` + secretColumns

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, key, encryptedValue))
	if err != nil {
		return s, fmt.Errorf("update secret %q: %w", key, classify(err))
	}
	return s, nil
}

// DeleteSecret deletes a secret and returns the deleted row.
func (d *Database) DeleteSecret(ctx context.Context, key string) (Secret, error) {
	const query = `
	DELETE FROM cove.secrets
	WHERE key = $1
	RETURNING ` + secretColumns

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, key))
	if err != nil {
		return s, fmt.Errorf("delete secret %q: %w", key, classify(err))
	}
	return s, nil
}

func (d *Database) LogEvent(ctx context.Context, logInfo EventLogInput) error {

	const query = `
	INSERT INTO cove.event_log
	(secret_id, secret_key, secret_version, kind, source, old_encrypted_value, new_encrypted_value, detail)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''));
	`

	_, err := d.Pool.Exec(ctx, query, logInfo.SecretID, logInfo.SecretKey, logInfo.SecretVersion, logInfo.Kind, logInfo.Source, logInfo.OldEncryptedValue, logInfo.NewEncryptedValue, logInfo.Detail)
	if err != nil {
		return fmt.Errorf("LogEvent: %w", err)
	}

	return nil
}

// RenameSecret changes a secret's key and returns the renamed row.
func (d *Database) RenameSecret(ctx context.Context, oldKey string, newKey string) (Secret, error) {
	const query = `
	UPDATE cove.secrets
	SET key = $2
	WHERE key = $1
	RETURNING ` + secretColumns

	s, err := scanSecret(d.Pool.QueryRow(ctx, query, oldKey, newKey))
	if err != nil {
		return s, fmt.Errorf("rename secret %q to %q: %w", oldKey, newKey, classify(err))
	}
	return s, nil
}

// eventColumns is the column list scanned by scanEvent, in order.
const eventColumns = `secret_key, secret_version, kind, source, COALESCE(detail, ''), occurred_at`

// ListEvents returns key's events, newest first. limit <= 0 returns them all.
func (d *Database) ListEvents(ctx context.Context, key string, limit int) ([]Event, error) {
	query := `
	SELECT ` + eventColumns + `
	FROM cove.event_log
	WHERE secret_key = $1
	ORDER BY occurred_at DESC, id DESC`
	args := []any{key}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events for %q: %w", key, err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// LastEvent returns key's most recent event of the given kind. found is
// false when there is none.
func (d *Database) LastEvent(ctx context.Context, key string, kind EventKind) (e Event, found bool, err error) {
	const query = `
	SELECT ` + eventColumns + `
	FROM cove.event_log
	WHERE secret_key = $1 AND kind = $2
	ORDER BY occurred_at DESC, id DESC
	LIMIT 1`

	e, err = scanEvent(d.Pool.QueryRow(ctx, query, key, kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, false, nil
	}
	if err != nil {
		return e, false, fmt.Errorf("last %s event for %q: %w", kind, key, err)
	}
	return e, true, nil
}

// ValueVersions returns every encrypted value key has had, by version, from
// the event log: the value written by each create/update, and the last value
// of a deleted secret. It returns ErrNotFound if key has no history.
func (d *Database) ValueVersions(ctx context.Context, key string) (map[int]string, error) {
	const query = `
	SELECT secret_version, COALESCE(new_encrypted_value, old_encrypted_value)
	FROM cove.event_log
	WHERE secret_key = $1
	  AND ((kind IN ('create', 'update') AND new_encrypted_value IS NOT NULL)
	    OR (kind = 'delete' AND old_encrypted_value IS NOT NULL))
	ORDER BY id`

	rows, err := d.Pool.Query(ctx, query, key)
	if err != nil {
		return nil, fmt.Errorf("value history for %q: %w", key, err)
	}
	defer rows.Close()

	versions := make(map[int]string)
	for rows.Next() {
		var version int
		var value string
		if err := rows.Scan(&version, &value); err != nil {
			return nil, fmt.Errorf("scan value history: %w", err)
		}
		versions[version] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("value history for %q: %w", key, ErrNotFound)
	}
	return versions, nil
}

// CountSecrets returns how many secrets the vault holds.
func (d *Database) CountSecrets(ctx context.Context) (int, error) {
	var n int
	if err := d.Pool.QueryRow(ctx, `SELECT count(*) FROM cove.secrets`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count secrets: %w", err)
	}
	return n, nil
}

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.SecretKey, &e.SecretVersion, &e.Kind, &e.Source, &e.Detail, &e.OccurredAt)
	return e, err
}

func scanSecret(row pgx.Row) (Secret, error) {
	var s Secret
	err := row.Scan(
		&s.ID,
		&s.Key,
		&s.EncryptedValue,
		&s.Version,
		&s.ReadCount,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	return s, err
}
