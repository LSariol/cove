package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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
		return s, fmt.Errorf("insert secret %q: %w", key, err)
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
		return s, fmt.Errorf("read secret %q: %w", key, err)
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
		return s, fmt.Errorf("select existing secret %q: %w", key, err)
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
		return s, fmt.Errorf("update secret %q: %w", key, err)
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
		if errors.Is(err, pgx.ErrNoRows) {
			return s, fmt.Errorf("no secret found with key %q", key)
		}
		return s, fmt.Errorf("delete secret %q: %w", key, err)
	}
	return s, nil
}

func (d *Database) LogEvent(ctx context.Context, logInfo EventLogInput) error {

	const query = `
	INSERT INTO cove.event_log
	(secret_id, secret_key, secret_version, kind, source, old_encrypted_value, new_encrypted_value)
	VALUES ($1, $2, $3, $4, $5, $6, $7);
	`

	_, err := d.Pool.Exec(ctx, query, logInfo.SecretID, logInfo.SecretKey, logInfo.SecretVersion, logInfo.Kind, logInfo.Source, logInfo.OldEncryptedValue, logInfo.NewEncryptedValue)
	if err != nil {
		return fmt.Errorf("LogEvent: %w", err)
	}

	return nil
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
