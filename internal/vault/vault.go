// Package vault holds Cove's rules for secrets. Values are encrypted before
// they're stored and decrypted when read, and every operation is recorded in
// the event log. The HTTP API and the CLI both go through a Vault.
//
// Each operation and its event-log entry run in one transaction: they're saved
// together or not at all. If the event can't be recorded, the operation fails.
package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
)

var (
	ErrNotFound      = database.ErrNotFound
	ErrAlreadyExists = database.ErrAlreadyExists

	// ErrDecrypt is returned by Get when a stored value can't be decrypted,
	// usually because VAULT_ENCRYPTION_KEY changed after it was stored.
	ErrDecrypt = errors.New("the stored value couldn't be decrypted")

	ErrVersionNotFound = errors.New("that version isn't in the secret's history")

	// ErrNothingToRestore is returned by Restore when there's no earlier
	// value, e.g. the secret is at version 1 or already has that value.
	ErrNothingToRestore = errors.New("there's no earlier value to restore")

	// ErrWrongKey means the vault is encrypted with a different key than
	// VAULT_ENCRYPTION_KEY: the wrong key is configured, or `cove rotate-key`
	// ran while this Cove was running.
	ErrWrongKey = errors.New("the vault is encrypted with a different key than VAULT_ENCRYPTION_KEY; if it was just rotated, restart Cove with the new key")
)

// Store is the persistence a Vault needs. *database.Database implements it.
type Store = database.Store

type Event = database.Event

// Info is a secret's details, without its value.
type Info struct {
	Secret
	LastRead *Event // nil if it has never been read
}

// Secret is a secret as seen by callers. Value is plaintext, and is only set
// by Get and Show.
type Secret struct {
	Key       string
	Value     string
	Version   int
	ReadCount int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Vault struct {
	store  Store
	cipher *encryption.Cipher
}

func New(store Store, cipher *encryption.Cipher) *Vault {
	return &Vault{
		store:  store,
		cipher: cipher,
	}
}

// Create encrypts value and stores it as a new secret.
func (v *Vault) Create(ctx context.Context, key string, value string, source string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}

	encryptedValue, err := v.cipher.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	return v.store.WithinTx(ctx, func(tx Store) error {
		created, err := tx.InsertSecret(ctx, key, encryptedValue)
		if err != nil {
			return err
		}

		if err := logEvent(ctx, tx, database.EventLogInput{
			SecretID:          created.ID,
			SecretKey:         created.Key,
			SecretVersion:     created.Version,
			Kind:              database.EventCreate,
			Source:            source,
			NewEncryptedValue: &created.EncryptedValue,
		}); err != nil {
			return err
		}
		return v.checkKey(ctx, tx)
	})
}

// Get returns a secret with its decrypted value, and counts it as a read. If
// the value can't be decrypted, nothing is counted or logged.
func (v *Vault) Get(ctx context.Context, key string, source string) (Secret, error) {
	return v.read(ctx, key, source, true)
}

// Show returns a secret with its decrypted value, like Get, but doesn't count
// as a read: read_count tracks how often apps pull a secret, and a person
// checking it in the CLI shouldn't change that. It's still logged as a read.
func (v *Vault) Show(ctx context.Context, key string, source string) (Secret, error) {
	return v.read(ctx, key, source, false)
}

func (v *Vault) read(ctx context.Context, key string, source string, countRead bool) (Secret, error) {
	var secret Secret

	err := v.store.WithinTx(ctx, func(tx Store) error {
		var s database.Secret
		var err error
		if countRead {
			s, err = tx.ReadSecret(ctx, key)
		} else {
			s, err = tx.GetSecret(ctx, key)
		}
		if err != nil {
			return err
		}

		value, err := v.cipher.Decrypt(s.EncryptedValue)
		if err != nil {
			return v.decryptError(ctx, tx, key, err)
		}

		secret = fromRow(s)
		secret.Value = value

		return logEvent(ctx, tx, database.EventLogInput{
			SecretID:      s.ID,
			SecretKey:     s.Key,
			SecretVersion: s.Version,
			Kind:          database.EventRead,
			Source:        source,
		})
	})
	if err != nil {
		return Secret{}, err
	}
	return secret, nil
}

// MissingError is returned by GetMany when some keys don't exist. Keys lists
// all of them, in the order asked for. It matches ErrNotFound.
type MissingError struct {
	Keys []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("no secret named %s", strings.Join(e.Keys, ", "))
}

func (e *MissingError) Is(target error) bool { return target == ErrNotFound }

// GetMany returns several secrets with their decrypted values, in the order
// asked for (duplicates once), and counts each as a read. It's all or
// nothing: if any key is missing it returns a *MissingError naming every
// missing key, and if any value can't be decrypted it fails; either way no
// read is counted or logged.
func (v *Vault) GetMany(ctx context.Context, keys []string, source string) ([]Secret, error) {
	keys = dedupe(keys)
	var secrets []Secret

	err := v.store.WithinTx(ctx, func(tx Store) error {
		secrets = make([]Secret, 0, len(keys))
		var missing []string

		for _, key := range keys {
			s, err := tx.ReadSecret(ctx, key)
			if errors.Is(err, ErrNotFound) {
				missing = append(missing, key)
				continue
			}
			if err != nil {
				return err
			}
			if len(missing) > 0 {
				continue // failing anyway; just find the other missing keys
			}

			value, err := v.cipher.Decrypt(s.EncryptedValue)
			if err != nil {
				return v.decryptError(ctx, tx, key, err)
			}
			secret := fromRow(s)
			secret.Value = value
			secrets = append(secrets, secret)

			if err := logEvent(ctx, tx, database.EventLogInput{
				SecretID:      s.ID,
				SecretKey:     s.Key,
				SecretVersion: s.Version,
				Kind:          database.EventRead,
				Source:        source,
			}); err != nil {
				return err
			}
		}

		if len(missing) > 0 {
			return &MissingError{Keys: missing}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func dedupe(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// List returns every secret without values, ordered by key.
func (v *Vault) List(ctx context.Context) ([]Secret, error) {
	rows, err := v.store.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}

	secrets := make([]Secret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, fromRow(row))
	}
	return secrets, nil
}

// Update encrypts value, replaces the secret's current value, and returns the
// updated secret (without its value). The row is locked while it's updated, so
// concurrent updates are applied, and logged, one after another.
func (v *Vault) Update(ctx context.Context, key string, value string, source string) (Secret, error) {
	encryptedValue, err := v.cipher.Encrypt(value)
	if err != nil {
		return Secret{}, fmt.Errorf("encrypt: %w", err)
	}

	var result Secret
	err = v.store.WithinTx(ctx, func(tx Store) error {
		old, err := tx.GetSecretForUpdate(ctx, key)
		if err != nil {
			return err
		}

		updated, err := tx.UpdateSecretValue(ctx, key, encryptedValue)
		if err != nil {
			return err
		}
		result = fromRow(updated)

		if err := logEvent(ctx, tx, database.EventLogInput{
			SecretID:          updated.ID,
			SecretKey:         updated.Key,
			SecretVersion:     updated.Version,
			Kind:              database.EventUpdate,
			Source:            source,
			OldEncryptedValue: &old.EncryptedValue,
			NewEncryptedValue: &updated.EncryptedValue,
		}); err != nil {
			return err
		}
		return v.checkKey(ctx, tx)
	})
	if err != nil {
		return Secret{}, err
	}
	return result, nil
}

// Delete removes a secret. Its last value remains in the event log.
func (v *Vault) Delete(ctx context.Context, key string, source string) error {
	return v.store.WithinTx(ctx, func(tx Store) error {
		deleted, err := tx.DeleteSecret(ctx, key)
		if err != nil {
			return err
		}

		return logEvent(ctx, tx, database.EventLogInput{
			SecretID:          deleted.ID,
			SecretKey:         deleted.Key,
			SecretVersion:     deleted.Version,
			Kind:              database.EventDelete,
			Source:            source,
			OldEncryptedValue: &deleted.EncryptedValue,
		})
	})
}

// Rename changes a secret's key, keeping its value, version and read count.
// The rename is logged under both keys, so either key's history shows it.
func (v *Vault) Rename(ctx context.Context, oldKey string, newKey string, source string) error {
	if err := ValidateKey(newKey); err != nil {
		return err
	}

	return v.store.WithinTx(ctx, func(tx Store) error {
		renamed, err := tx.RenameSecret(ctx, oldKey, newKey)
		if err != nil {
			return err
		}

		for _, e := range []struct{ key, detail string }{
			{oldKey, "renamed to " + newKey},
			{newKey, "renamed from " + oldKey},
		} {
			err := logEvent(ctx, tx, database.EventLogInput{
				SecretID:      renamed.ID,
				SecretKey:     e.key,
				SecretVersion: renamed.Version,
				Kind:          database.EventRename,
				Source:        source,
				Detail:        e.detail,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Restore brings back an earlier value of key. version picks which one;
// version <= 0 means the value before the current one, or for a deleted secret
// its last value. The value is written as a new version (or recreates a
// deleted secret), so nothing is overwritten or lost. It returns the secret
// after the restore and the version the value came from.
func (v *Vault) Restore(ctx context.Context, key string, version int, source string) (Secret, int, error) {
	var result Secret
	var target int

	err := v.store.WithinTx(ctx, func(tx Store) error {
		current, err := tx.GetSecretForUpdate(ctx, key)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		versions, err := tx.ValueVersions(ctx, key)
		if err != nil {
			return err
		}

		target = version
		if target <= 0 {
			if exists {
				target = current.Version - 1
			} else {
				for ver := range versions {
					target = max(target, ver)
				}
			}
		}

		if exists && target == current.Version {
			return fmt.Errorf("%q is already at version %d: %w", key, target, ErrNothingToRestore)
		}
		encrypted, ok := versions[target]
		if !ok {
			if version <= 0 {
				return ErrNothingToRestore
			}
			return fmt.Errorf("version %d of %q: %w", target, key, ErrVersionNotFound)
		}

		value, err := v.cipher.Decrypt(encrypted)
		if err != nil {
			return v.decryptError(ctx, tx, key, err)
		}
		reencrypted, err := v.cipher.Encrypt(value)
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}

		detail := fmt.Sprintf("restored version %d", target)

		if exists {
			updated, err := tx.UpdateSecretValue(ctx, key, reencrypted)
			if err != nil {
				return err
			}
			result = fromRow(updated)
			if err := logEvent(ctx, tx, database.EventLogInput{
				SecretID:          updated.ID,
				SecretKey:         updated.Key,
				SecretVersion:     updated.Version,
				Kind:              database.EventUpdate,
				Source:            source,
				OldEncryptedValue: &current.EncryptedValue,
				NewEncryptedValue: &updated.EncryptedValue,
				Detail:            detail,
			}); err != nil {
				return err
			}
			return v.checkKey(ctx, tx)
		}

		created, err := tx.InsertSecret(ctx, key, reencrypted)
		if err != nil {
			return err
		}
		result = fromRow(created)
		if err := logEvent(ctx, tx, database.EventLogInput{
			SecretID:          created.ID,
			SecretKey:         created.Key,
			SecretVersion:     created.Version,
			Kind:              database.EventCreate,
			Source:            source,
			NewEncryptedValue: &created.EncryptedValue,
			Detail:            detail + " of the deleted secret",
		}); err != nil {
			return err
		}
		return v.checkKey(ctx, tx)
	})
	if err != nil {
		return Secret{}, 0, err
	}
	return result, target, nil
}

// History returns key's events, newest first; limit <= 0 returns them all. It
// works for deleted secrets too, since the event log keeps their history.
func (v *Vault) History(ctx context.Context, key string, limit int) ([]Event, error) {
	return v.store.ListEvents(ctx, key, limit)
}

// Info returns a secret's details and when it was last read, without its value.
func (v *Vault) Info(ctx context.Context, key string) (Info, error) {
	s, err := v.store.GetSecret(ctx, key)
	if err != nil {
		return Info{}, err
	}

	info := Info{Secret: fromRow(s)}

	lastRead, found, err := v.store.LastEvent(ctx, key, database.EventRead)
	if err != nil {
		return Info{}, err
	}
	if found {
		info.LastRead = &lastRead
	}
	return info, nil
}

// KeyStatus describes the vault's encryption key, for `status`.
type KeyStatus struct {
	Fingerprint string     // of VAULT_ENCRYPTION_KEY
	Recorded    bool       // whether the vault has recorded its key yet
	Matches     bool       // whether it's the recorded key
	RotatedAt   *time.Time // when the vault was last rotated, if ever
}

// KeyStatus compares VAULT_ENCRYPTION_KEY with the vault's recorded key.
func (v *Vault) KeyStatus(ctx context.Context) (KeyStatus, error) {
	st := KeyStatus{Fingerprint: v.cipher.Fingerprint()}
	k, found, err := v.store.VaultKey(ctx)
	if err != nil {
		return st, err
	}
	st.Recorded, st.Matches, st.RotatedAt = found, found && k.Fingerprint == st.Fingerprint, k.RotatedAt
	return st, nil
}

// EnsureKey makes sure VAULT_ENCRYPTION_KEY is the key the vault is
// encrypted with. The first time (a new vault, or the first start after
// upgrading), it checks that the key decrypts a stored secret, then records
// its fingerprint. After that, a different key returns ErrWrongKey, so Cove
// doesn't start with the wrong key.
func (v *Vault) EnsureKey(ctx context.Context) error {
	st, err := v.KeyStatus(ctx)
	if err != nil {
		return err
	}
	if st.Recorded {
		if !st.Matches {
			return ErrWrongKey
		}
		return nil
	}

	rows, err := v.store.ListSecrets(ctx)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		s, err := v.store.GetSecret(ctx, rows[0].Key)
		if err != nil {
			return err
		}
		if _, err := v.cipher.Decrypt(s.EncryptedValue); err != nil {
			return fmt.Errorf("VAULT_ENCRYPTION_KEY can't decrypt the stored secrets (tried %q): is it the right key?", s.Key)
		}
	}
	if err := v.store.RecordVaultKey(ctx, st.Fingerprint); err != nil {
		return err
	}

	// Another Cove may have recorded one at the same moment.
	if st, err = v.KeyStatus(ctx); err != nil {
		return err
	}
	if !st.Matches {
		return ErrWrongKey
	}
	return nil
}

// checkKey fails a write when the vault's recorded key isn't this Cove's
// key, e.g. because it was rotated while Cove was running. It runs at the end
// of each write transaction, after the write has taken its locks, so it
// always sees a rotation that committed before the write could proceed.
func (v *Vault) checkKey(ctx context.Context, tx Store) error {
	k, found, err := tx.VaultKey(ctx)
	if err != nil {
		return err
	}
	if found && k.Fingerprint != v.cipher.Fingerprint() {
		return ErrWrongKey
	}
	return nil
}

// decryptError explains a value that can't be decrypted: the wrong key, or
// a damaged value.
func (v *Vault) decryptError(ctx context.Context, tx Store, key string, cause error) error {
	if err := v.checkKey(ctx, tx); errors.Is(err, ErrWrongKey) {
		return fmt.Errorf("decrypt %q: %w (%w)", key, ErrWrongKey, ErrDecrypt)
	}
	return fmt.Errorf("get secret %q: %w (%v)", key, ErrDecrypt, cause)
}

// logEvent records an event, and explains a failure: the caller's transaction
// is then rolled back, so nothing happens without a record of it.
func logEvent(ctx context.Context, tx Store, e database.EventLogInput) error {
	if err := tx.LogEvent(ctx, e); err != nil {
		return fmt.Errorf("record the %s event for %q (nothing was changed): %w", e.Kind, e.SecretKey, err)
	}
	return nil
}

func fromRow(s database.Secret) Secret {
	return Secret{
		Key:       s.Key,
		Version:   s.Version,
		ReadCount: s.ReadCount,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
