package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/jackc/pgx/v5"
)

// CreateSecret takes a secret and stores it into the Cove database
func (d *Database) CreateSecret(ctx context.Context, key string, value string, source string) error {
	var logInput EventLogInput
	var insertSecret Secret

	const query = `
	INSERT INTO cove.secrets (secret_key, secret_value)
	VALUES ($1, $2)
	RETURNING
		id,
		secret_key,
		secret_value,
		version;
`

	encryptedValue, err := encryption.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	insertSecret.Key = key
	insertSecret.Value = encryptedValue

	row := d.Pool.QueryRow(ctx, query, insertSecret.Key, insertSecret.Value)

	err = row.Scan(
		&logInput.SecretID,
		&logInput.SecretKey,
		&logInput.NewValue,
		&logInput.Version,
	)

	logInput.Source = source
	logInput.Modification = EventCreate
	logInput.OldValue = nil

	if err != nil {
		return fmt.Errorf("row.scan: %w", err)
	}

	_ = d.LogEvent(ctx, logInput)

	return nil
}

func (d *Database) GetSecret(ctx context.Context, key string, source string) (Secret, error) {
	var s Secret

	const query = `
	UPDATE cove.secrets
	SET times_pulled = times_pulled + 1
	WHERE secret_key = $1
	RETURNING
		id,
		secret_key,
		secret_value,
		version,
		times_pulled,
		date_added,
		last_modified;
`

	row := d.Pool.QueryRow(ctx, query, key)

	err := row.Scan(
		&s.Id,
		&s.Key,
		&s.Value,
		&s.Version,
		&s.TimesPulled,
		&s.DateAdded,
		&s.LastModified)

	if err != nil {
		return s, fmt.Errorf("row.scan %q: %w", key, err)
	}

	decryptedVal, err := encryption.Decrypt(s.Value)
	if err != nil {
		return s, fmt.Errorf("get secret %q: %w", key, err)
	}

	logInfo := EventLogInput{
		SecretID:     s.Id,
		SecretKey:    s.Key,
		Version:      s.Version,
		Modification: EventRead,
		Source:       source,
		OldValue:     &s.Value,
		NewValue:     nil,
	}

	_ = d.LogEvent(ctx, logInfo)

	s.Value = decryptedVal

	return s, nil
}

func (d *Database) GetAllKeys(ctx context.Context) ([]Secret, error) {
	var secrets []Secret

	const query = `
	SELECT id, secret_key, secret_value, version, times_pulled, date_added, last_modified
	FROM cove.secrets 
	ORDER BY secret_key ASC
	`

	rows, err := d.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query secrets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s Secret
		if err := rows.Scan(
			&s.Id,
			&s.Key,
			&s.Value,
			&s.Version,
			&s.TimesPulled,
			&s.DateAdded,
			&s.LastModified,
		); err != nil {
			return nil, fmt.Errorf("scan secret: %w", err)
		}

		s.Value = ""
		secrets = append(secrets, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return secrets, nil
}

func (d *Database) UpdateSecret(ctx context.Context, key string, value string, source string) error {

	const querySelect = `
	SELECT
		id,
		secret_key,
		secret_value,
		version,
		times_pulled,
		date_added,
		last_modified
	FROM cove.secrets
	WHERE secret_key = $1;
	`

	const queryUpdate = `
	UPDATE cove.secrets
	SET
		secret_value = $2,
		version = version + 1,
		last_modified = now()
	WHERE secret_key = $1
	RETURNING
		id,
		secret_key,
		secret_value,
		version,
		times_pulled,
		date_added,
		last_modified;
	`

	var oldSecret Secret
	err := d.Pool.QueryRow(ctx, querySelect, key).Scan(
		&oldSecret.Id,
		&oldSecret.Key,
		&oldSecret.Value,
		&oldSecret.Version,
		&oldSecret.TimesPulled,
		&oldSecret.DateAdded,
		&oldSecret.LastModified,
	)
	if err != nil {
		return fmt.Errorf("select existing secret %q: %w", key, err)
	}

	encryptedVal, err := encryption.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	var newSecret Secret
	err = d.Pool.QueryRow(ctx, queryUpdate, key, encryptedVal).Scan(
		&newSecret.Id,
		&newSecret.Key,
		&newSecret.Value,
		&newSecret.Version,
		&newSecret.TimesPulled,
		&newSecret.DateAdded,
		&newSecret.LastModified,
	)
	if err != nil {
		return fmt.Errorf("update secret %q: %w", key, err)
	}
	oldVal := oldSecret.Value
	newVal := newSecret.Value

	logInfo := EventLogInput{
		SecretID:     newSecret.Id,
		SecretKey:    newSecret.Key,
		Version:      newSecret.Version,
		Modification: EventUpdate,
		Source:       source,
		OldValue:     &oldVal,
		NewValue:     &newVal,
	}

	_ = d.LogEvent(ctx, logInfo)

	return nil
}

func (d *Database) DeleteSecret(ctx context.Context, key string, source string) error {
	const query = `
	DELETE FROM cove.secrets
	WHERE secret_key = $1
	RETURNING
		id,
		secret_key,
		secret_value,
		version,
		times_pulled,
		date_added,
		last_modified;
	`
	var deleted Secret

	row := d.Pool.QueryRow(ctx, query, key)

	err := row.Scan(
		&deleted.Id,
		&deleted.Key,
		&deleted.Value,
		&deleted.Version,
		&deleted.TimesPulled,
		&deleted.DateAdded,
		&deleted.LastModified,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no secret found with key %q", key)
		}
		return fmt.Errorf("delete scan %q: %w", key, err)
	}

	oldVal := deleted.Value

	logInfo := EventLogInput{
		SecretID:     deleted.Id,
		SecretKey:    deleted.Key,
		Version:      deleted.Version,
		Modification: EventDelete,
		Source:       source,
		OldValue:     &oldVal,
		NewValue:     nil,
	}

	_ = d.LogEvent(ctx, logInfo)

	return nil
}

func (d *Database) LogEvent(ctx context.Context, logInfo EventLogInput) error {

	const query = `
	INSERT INTO cove.event_log 
	(secret_id, secret_key, version, modification, source, old_value, new_value)
	VALUES ($1, $2, $3, $4, $5, $6, $7);
	`

	_, err := d.Pool.Exec(ctx, query, logInfo.SecretID, logInfo.SecretKey, logInfo.Version, logInfo.Modification, logInfo.Source, logInfo.OldValue, logInfo.NewValue)
	if err != nil {
		return fmt.Errorf("LogEvent: %w", err)
	}

	return nil
}
