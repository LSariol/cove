// Package vault holds Cove's rules for secrets. Values are encrypted before
// they're stored and decrypted when read, and every operation is recorded in
// the event log. The HTTP API and the CLI both go through a Vault.
package vault

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
)

var (
	// ErrNotFound is returned when no secret has the requested key.
	ErrNotFound = database.ErrNotFound

	// ErrAlreadyExists is returned by Create when the key is already taken.
	ErrAlreadyExists = database.ErrAlreadyExists

	// ErrDecrypt is returned by Get when a stored value can't be decrypted,
	// usually because VAULT_ENCRYPTION_KEY changed after it was stored.
	ErrDecrypt = errors.New("the stored value couldn't be decrypted")

	// ErrVersionNotFound is returned by Restore when the requested version
	// isn't in the secret's history.
	ErrVersionNotFound = errors.New("that version isn't in the secret's history")

	// ErrNothingToRestore is returned by Restore when there's no earlier
	// value, e.g. the secret is at version 1 or already has that value.
	ErrNothingToRestore = errors.New("there's no earlier value to restore")
)

// Store is the persistence a Vault needs. *database.Database implements it.
type Store interface {
	InsertSecret(ctx context.Context, key string, encryptedValue string) (database.Secret, error)
	ReadSecret(ctx context.Context, key string) (database.Secret, error)
	GetSecret(ctx context.Context, key string) (database.Secret, error)
	ListSecrets(ctx context.Context) ([]database.Secret, error)
	UpdateSecretValue(ctx context.Context, key string, encryptedValue string) (database.Secret, error)
	DeleteSecret(ctx context.Context, key string) (database.Secret, error)
	LogEvent(ctx context.Context, logInfo database.EventLogInput) error
	ListEvents(ctx context.Context, key string, limit int) ([]database.Event, error)
	LastEvent(ctx context.Context, key string, kind database.EventKind) (database.Event, bool, error)
	RenameSecret(ctx context.Context, oldKey string, newKey string) (database.Secret, error)
	ValueVersions(ctx context.Context, key string) (map[int]string, error)
}

// Event is one entry in a secret's history.
type Event = database.Event

// Info is a secret's details, without its value.
type Info struct {
	Secret
	LastRead *Event // nil if it has never been read
}

// Secret is a secret as seen by callers. Value is plaintext, and is only set
// by Get.
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

	created, err := v.store.InsertSecret(ctx, key, encryptedValue)
	if err != nil {
		return err
	}

	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          created.ID,
		SecretKey:         created.Key,
		SecretVersion:     created.Version,
		Kind:              database.EventCreate,
		Source:            source,
		OldEncryptedValue: nil,
		NewEncryptedValue: &created.EncryptedValue,
	})

	return nil
}

// Get returns a secret with its decrypted value, and counts it as a read.
func (v *Vault) Get(ctx context.Context, key string, source string) (Secret, error) {
	s, err := v.store.ReadSecret(ctx, key)
	if err != nil {
		return Secret{}, err
	}

	value, err := v.cipher.Decrypt(s.EncryptedValue)
	if err != nil {
		return Secret{}, fmt.Errorf("get secret %q: %w (%v)", key, ErrDecrypt, err)
	}

	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          s.ID,
		SecretKey:         s.Key,
		SecretVersion:     s.Version,
		Kind:              database.EventRead,
		Source:            source,
		OldEncryptedValue: &s.EncryptedValue,
		NewEncryptedValue: nil,
	})

	secret := fromRow(s)
	secret.Value = value
	return secret, nil
}

// Show returns a secret with its decrypted value, like Get, but doesn't count
// as a read: read_count tracks how often apps pull a secret, and a person
// checking it in the CLI shouldn't change that. It's still logged as a read.
func (v *Vault) Show(ctx context.Context, key string, source string) (Secret, error) {
	s, err := v.store.GetSecret(ctx, key)
	if err != nil {
		return Secret{}, err
	}

	value, err := v.cipher.Decrypt(s.EncryptedValue)
	if err != nil {
		return Secret{}, fmt.Errorf("get secret %q: %w (%v)", key, ErrDecrypt, err)
	}

	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          s.ID,
		SecretKey:         s.Key,
		SecretVersion:     s.Version,
		Kind:              database.EventRead,
		Source:            source,
		OldEncryptedValue: &s.EncryptedValue,
		NewEncryptedValue: nil,
	})

	secret := fromRow(s)
	secret.Value = value
	return secret, nil
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
// updated secret (without its value).
func (v *Vault) Update(ctx context.Context, key string, value string, source string) (Secret, error) {
	old, err := v.store.GetSecret(ctx, key)
	if err != nil {
		return Secret{}, err
	}

	encryptedValue, err := v.cipher.Encrypt(value)
	if err != nil {
		return Secret{}, fmt.Errorf("encrypt: %w", err)
	}

	updated, err := v.store.UpdateSecretValue(ctx, key, encryptedValue)
	if err != nil {
		return Secret{}, err
	}

	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          updated.ID,
		SecretKey:         updated.Key,
		SecretVersion:     updated.Version,
		Kind:              database.EventUpdate,
		Source:            source,
		OldEncryptedValue: &old.EncryptedValue,
		NewEncryptedValue: &updated.EncryptedValue,
	})

	return fromRow(updated), nil
}

// Delete removes a secret. Its last value remains in the event log.
func (v *Vault) Delete(ctx context.Context, key string, source string) error {
	deleted, err := v.store.DeleteSecret(ctx, key)
	if err != nil {
		return err
	}

	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          deleted.ID,
		SecretKey:         deleted.Key,
		SecretVersion:     deleted.Version,
		Kind:              database.EventDelete,
		Source:            source,
		OldEncryptedValue: &deleted.EncryptedValue,
		NewEncryptedValue: nil,
	})

	return nil
}

// Rename changes a secret's key, keeping its value, version and read count.
// The rename is logged under both keys, so either key's history shows it.
func (v *Vault) Rename(ctx context.Context, oldKey string, newKey string, source string) error {
	if err := ValidateKey(newKey); err != nil {
		return err
	}

	renamed, err := v.store.RenameSecret(ctx, oldKey, newKey)
	if err != nil {
		return err
	}

	for _, e := range []struct{ key, detail string }{
		{oldKey, "renamed to " + newKey},
		{newKey, "renamed from " + oldKey},
	} {
		_ = v.store.LogEvent(ctx, database.EventLogInput{
			SecretID:      renamed.ID,
			SecretKey:     e.key,
			SecretVersion: renamed.Version,
			Kind:          database.EventRename,
			Source:        source,
			Detail:        e.detail,
		})
	}
	return nil
}

// Restore brings back an earlier value of key. version picks which one;
// version <= 0 means the value before the current one, or for a deleted secret
// its last value. The value is written as a new version (or recreates a
// deleted secret), so nothing is overwritten or lost. It returns the secret
// after the restore and the version the value came from.
func (v *Vault) Restore(ctx context.Context, key string, version int, source string) (Secret, int, error) {
	current, err := v.store.GetSecret(ctx, key)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Secret{}, 0, err
	}

	versions, err := v.store.ValueVersions(ctx, key)
	if err != nil {
		return Secret{}, 0, err
	}

	target := version
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
		return Secret{}, 0, fmt.Errorf("%q is already at version %d: %w", key, target, ErrNothingToRestore)
	}
	encrypted, ok := versions[target]
	if !ok {
		if version <= 0 {
			return Secret{}, 0, ErrNothingToRestore
		}
		return Secret{}, 0, fmt.Errorf("version %d of %q: %w", target, key, ErrVersionNotFound)
	}

	value, err := v.cipher.Decrypt(encrypted)
	if err != nil {
		return Secret{}, 0, fmt.Errorf("restore %q: %w (%v)", key, ErrDecrypt, err)
	}
	reencrypted, err := v.cipher.Encrypt(value)
	if err != nil {
		return Secret{}, 0, fmt.Errorf("encrypt: %w", err)
	}

	detail := fmt.Sprintf("restored version %d", target)

	if exists {
		updated, err := v.store.UpdateSecretValue(ctx, key, reencrypted)
		if err != nil {
			return Secret{}, 0, err
		}
		_ = v.store.LogEvent(ctx, database.EventLogInput{
			SecretID:          updated.ID,
			SecretKey:         updated.Key,
			SecretVersion:     updated.Version,
			Kind:              database.EventUpdate,
			Source:            source,
			OldEncryptedValue: &current.EncryptedValue,
			NewEncryptedValue: &updated.EncryptedValue,
			Detail:            detail,
		})
		return fromRow(updated), target, nil
	}

	created, err := v.store.InsertSecret(ctx, key, reencrypted)
	if err != nil {
		return Secret{}, 0, err
	}
	_ = v.store.LogEvent(ctx, database.EventLogInput{
		SecretID:          created.ID,
		SecretKey:         created.Key,
		SecretVersion:     created.Version,
		Kind:              database.EventCreate,
		Source:            source,
		NewEncryptedValue: &created.EncryptedValue,
		Detail:            detail + " of the deleted secret",
	})
	return fromRow(created), target, nil
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

func fromRow(s database.Secret) Secret {
	return Secret{
		Key:       s.Key,
		Version:   s.Version,
		ReadCount: s.ReadCount,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
