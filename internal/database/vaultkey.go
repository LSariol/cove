package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// VaultKey is the row of cove.vault_key: a fingerprint of the key the vault
// is encrypted with, never the key itself.
type VaultKey struct {
	Fingerprint string
	RecordedAt  time.Time
	RotatedAt   *time.Time // nil if never rotated
}

// VaultKey returns the recorded key fingerprint. found is false before Cove
// has recorded one (a new vault, or the first start after upgrading).
func (d *Database) VaultKey(ctx context.Context) (k VaultKey, found bool, err error) {
	const query = `SELECT fingerprint, recorded_at, rotated_at FROM cove.vault_key`

	err = d.conn().QueryRow(ctx, query).Scan(&k.Fingerprint, &k.RecordedAt, &k.RotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, false, nil
	}
	if err != nil {
		return k, false, fmt.Errorf("read the vault key fingerprint: %w", err)
	}
	return k, true, nil
}

// RecordVaultKey records fingerprint as the vault's key, unless one is
// already recorded (then nothing changes; compare with VaultKey).
func (d *Database) RecordVaultKey(ctx context.Context, fingerprint string) error {
	const query = `INSERT INTO cove.vault_key (fingerprint) VALUES ($1) ON CONFLICT (id) DO NOTHING`

	if _, err := d.conn().Exec(ctx, query, fingerprint); err != nil {
		return fmt.Errorf("record the vault key fingerprint: %w", err)
	}
	return nil
}

// RotationResult says what RotateKey re-encrypted.
type RotationResult struct {
	Secrets     int // values in cove.secrets
	EventValues int // value copies in cove.event_log
}

// RotateKey re-encrypts every stored value and records the new key's
// fingerprint, all in one transaction: if anything fails, nothing changes.
// It connects with migrateURL (as cove_migrator, acting as cove_owner),
// because the event log is append-only for cove_app.
//
// reencrypt decrypts a value with the old key and encrypts it with the new
// one. oldFingerprint must be the recorded one, so the vault really is
// encrypted with the old key.
//
// While it runs, it locks cove.secrets and cove.event_log against writes.
// A running Cove's writes wait, then fail its key check, so a value can never
// be written with the old key after the rotation.
func RotateKey(ctx context.Context, migrateURL string, oldFingerprint string, newFingerprint string, reencrypt func(string) (string, error)) (RotationResult, error) {
	var result RotationResult

	conn, err := pgx.Connect(ctx, migrateURL)
	if err != nil {
		return result, fmt.Errorf("connect with COVE_MIGRATE_DATABASE_URL: %w", err)
	}
	defer conn.Close(ctx)

	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE cove.secrets, cove.event_log IN EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("lock the vault: %w", err)
		}

		var current string
		err := tx.QueryRow(ctx, `SELECT fingerprint FROM cove.vault_key FOR UPDATE`).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("Cove hasn't recorded its key yet: start the Cove server once with VAULT_ENCRYPTION_KEY, then rotate")
		}
		if err != nil {
			return fmt.Errorf("read the vault key fingerprint: %w", err)
		}
		if current == newFingerprint {
			return errors.New("the vault is already encrypted with VAULT_NEW_ENCRYPTION_KEY: move it to VAULT_ENCRYPTION_KEY and restart Cove")
		}
		if current != oldFingerprint {
			return errors.New("VAULT_ENCRYPTION_KEY isn't the key this vault is encrypted with, so it can't be rotated")
		}

		var failed []string
		redo := func(what string, value string) (string, bool) {
			out, err := reencrypt(value)
			if err != nil {
				failed = append(failed, what)
				return "", false
			}
			return out, true
		}

		// Read everything first: a connection can't run updates while it's
		// still reading rows.
		type row struct {
			id  string
			old *string
			new *string
		}

		secretRows, err := tx.Query(ctx, `SELECT id::text, key, encrypted_value FROM cove.secrets`)
		if err != nil {
			return fmt.Errorf("read secrets: %w", err)
		}
		var secrets []row
		for secretRows.Next() {
			var id, key, value string
			if err := secretRows.Scan(&id, &key, &value); err != nil {
				secretRows.Close()
				return fmt.Errorf("read secrets: %w", err)
			}
			if out, ok := redo("secret "+key, value); ok {
				secrets = append(secrets, row{id: id, new: &out})
			}
		}
		secretRows.Close()
		if err := secretRows.Err(); err != nil {
			return fmt.Errorf("read secrets: %w", err)
		}

		eventRows, err := tx.Query(ctx, `
			SELECT id::text, secret_key, old_encrypted_value, new_encrypted_value
			FROM cove.event_log
			WHERE old_encrypted_value IS NOT NULL OR new_encrypted_value IS NOT NULL`)
		if err != nil {
			return fmt.Errorf("read the event log: %w", err)
		}
		var events []row
		for eventRows.Next() {
			var id, key string
			var old, new *string
			if err := eventRows.Scan(&id, &key, &old, &new); err != nil {
				eventRows.Close()
				return fmt.Errorf("read the event log: %w", err)
			}
			r := row{id: id}
			what := fmt.Sprintf("event %s (%s)", id, key)
			if old != nil {
				if out, ok := redo(what, *old); ok {
					r.old = &out
				}
			}
			if new != nil {
				if out, ok := redo(what, *new); ok {
					r.new = &out
				}
			}
			events = append(events, r)
		}
		eventRows.Close()
		if err := eventRows.Err(); err != nil {
			return fmt.Errorf("read the event log: %w", err)
		}

		if len(failed) > 0 {
			shown := failed
			if len(shown) > 5 {
				shown = shown[:5]
			}
			return fmt.Errorf("%d stored values can't be decrypted with VAULT_ENCRYPTION_KEY (%s); nothing was changed",
				len(failed), strings.Join(shown, ", "))
		}

		for _, r := range secrets {
			if _, err := tx.Exec(ctx, `UPDATE cove.secrets SET encrypted_value = $2 WHERE id = $1::uuid`, r.id, *r.new); err != nil {
				return fmt.Errorf("re-encrypt a secret: %w", err)
			}
			result.Secrets++
		}
		for _, r := range events {
			_, err := tx.Exec(ctx, `
				UPDATE cove.event_log
				SET old_encrypted_value = COALESCE($2, old_encrypted_value),
				    new_encrypted_value = COALESCE($3, new_encrypted_value)
				WHERE id = $1::bigint`, r.id, r.old, r.new)
			if err != nil {
				return fmt.Errorf("re-encrypt an event log value: %w", err)
			}
			if r.old != nil {
				result.EventValues++
			}
			if r.new != nil {
				result.EventValues++
			}
		}

		_, err = tx.Exec(ctx, `UPDATE cove.vault_key SET fingerprint = $1, rotated_at = now()`, newFingerprint)
		if err != nil {
			return fmt.Errorf("record the new key's fingerprint: %w", err)
		}
		return nil
	})
	if err != nil {
		return RotationResult{}, err
	}
	return result, nil
}
