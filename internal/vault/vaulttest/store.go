// Package vaulttest provides an in-memory vault.Store for tests, so code built
// on a Vault can be tested without Postgres.
package vaulttest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/LSariol/Cove/internal/database"
)

// Store is an in-memory vault.Store. The zero value is not usable; call NewStore.
type Store struct {
	mu      sync.Mutex
	secrets map[string]database.Secret
	nextID  int

	Events []database.EventLogInput
	times  []time.Time

	// FailLogEvent makes LogEvent fail, to test that operations are rolled
	// back when their event can't be recorded.
	FailLogEvent bool

	vaultKey *database.VaultKey
}

// VaultKey returns the recorded key fingerprint, like cove.vault_key.
func (s *Store) VaultKey(ctx context.Context) (database.VaultKey, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultKey == nil {
		return database.VaultKey{}, false, nil
	}
	return *s.vaultKey, true, nil
}

// RecordVaultKey records fingerprint unless one is already recorded.
func (s *Store) RecordVaultKey(ctx context.Context, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultKey == nil {
		s.vaultKey = &database.VaultKey{Fingerprint: fingerprint, RecordedAt: time.Now()}
	}
	return nil
}

// SetVaultKey replaces the recorded fingerprint, like `cove rotate-key` would
// from another process.
func (s *Store) SetVaultKey(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.vaultKey = &database.VaultKey{Fingerprint: fingerprint, RecordedAt: now, RotatedAt: &now}
}

// ErrLogEvent is what LogEvent returns while FailLogEvent is set.
var ErrLogEvent = errors.New("vaulttest: event log unavailable")

// WithinTx runs fn and, like a real transaction, undoes all of fn's changes
// if it returns an error.
func (s *Store) WithinTx(ctx context.Context, fn func(tx database.Store) error) error {
	s.mu.Lock()
	secrets := maps.Clone(s.secrets)
	nextID, events, times := s.nextID, len(s.Events), len(s.times)
	s.mu.Unlock()

	if err := fn(s); err != nil {
		s.mu.Lock()
		s.secrets, s.nextID = secrets, nextID
		s.Events, s.times = s.Events[:events], s.times[:times]
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) GetSecretForUpdate(ctx context.Context, key string) (database.Secret, error) {
	return s.GetSecret(ctx, key)
}

func NewStore() *Store {
	return &Store{secrets: make(map[string]database.Secret)}
}

func (s *Store) InsertSecret(ctx context.Context, key string, encryptedValue string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.secrets[key]; ok {
		return database.Secret{}, fmt.Errorf("insert secret %q: %w", key, database.ErrAlreadyExists)
	}

	s.nextID++
	now := time.Now()
	secret := database.Secret{
		ID:             fmt.Sprintf("id-%d", s.nextID),
		Key:            key,
		EncryptedValue: encryptedValue,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.secrets[key] = secret
	return secret, nil
}

func (s *Store) ReadSecret(ctx context.Context, key string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[key]
	if !ok {
		return database.Secret{}, fmt.Errorf("read secret %q: %w", key, database.ErrNotFound)
	}
	secret.ReadCount++
	s.secrets[key] = secret
	return secret, nil
}

func (s *Store) GetSecret(ctx context.Context, key string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[key]
	if !ok {
		return database.Secret{}, fmt.Errorf("get secret %q: %w", key, database.ErrNotFound)
	}
	return secret, nil
}

func (s *Store) ListSecrets(ctx context.Context) ([]database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secrets := make([]database.Secret, 0, len(s.secrets))
	for _, secret := range s.secrets {
		secret.EncryptedValue = ""
		secrets = append(secrets, secret)
	}
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Key < secrets[j].Key })
	return secrets, nil
}

func (s *Store) UpdateSecretValue(ctx context.Context, key string, encryptedValue string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[key]
	if !ok {
		return database.Secret{}, fmt.Errorf("update secret %q: %w", key, database.ErrNotFound)
	}
	secret.EncryptedValue = encryptedValue
	secret.Version++
	secret.UpdatedAt = time.Now()
	s.secrets[key] = secret
	return secret, nil
}

func (s *Store) DeleteSecret(ctx context.Context, key string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[key]
	if !ok {
		return database.Secret{}, fmt.Errorf("delete secret %q: %w", key, database.ErrNotFound)
	}
	delete(s.secrets, key)
	return secret, nil
}

func (s *Store) RenameSecret(ctx context.Context, oldKey string, newKey string) (database.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[oldKey]
	if !ok {
		return database.Secret{}, fmt.Errorf("rename secret %q: %w", oldKey, database.ErrNotFound)
	}
	if _, taken := s.secrets[newKey]; taken {
		return database.Secret{}, fmt.Errorf("rename secret to %q: %w", newKey, database.ErrAlreadyExists)
	}

	delete(s.secrets, oldKey)
	secret.Key = newKey
	s.secrets[newKey] = secret
	return secret, nil
}

func (s *Store) ValueVersions(ctx context.Context, key string) (map[int]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Like the real query: events under key, plus every event of the secret
	// key last belonged to (a rename keeps the secret's id).
	var latestID string
	for _, e := range s.Events {
		if e.SecretKey == key && e.SecretID != "" {
			latestID = e.SecretID
		}
	}

	versions := make(map[int]string)
	for _, e := range s.Events {
		if e.SecretKey != key && (latestID == "" || e.SecretID != latestID) {
			continue
		}
		switch {
		case (e.Kind == database.EventCreate || e.Kind == database.EventUpdate) && e.NewEncryptedValue != nil:
			versions[e.SecretVersion] = *e.NewEncryptedValue
		case e.Kind == database.EventDelete && e.OldEncryptedValue != nil:
			versions[e.SecretVersion] = *e.OldEncryptedValue
		}
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("value history for %q: %w", key, database.ErrNotFound)
	}
	return versions, nil
}

func (s *Store) LogEvent(ctx context.Context, logInfo database.EventLogInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.FailLogEvent {
		return ErrLogEvent
	}
	s.Events = append(s.Events, logInfo)
	s.times = append(s.times, time.Now())
	return nil
}

func (s *Store) ListEvents(ctx context.Context, key string, limit int) ([]database.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var events []database.Event
	for i := len(s.Events) - 1; i >= 0; i-- {
		if s.Events[i].SecretKey != key {
			continue
		}
		events = append(events, s.event(i))
		if limit > 0 && len(events) == limit {
			break
		}
	}
	return events, nil
}

func (s *Store) LastEvent(ctx context.Context, key string, kind database.EventKind) (database.Event, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.Events) - 1; i >= 0; i-- {
		if s.Events[i].SecretKey == key && s.Events[i].Kind == kind {
			return s.event(i), true, nil
		}
	}
	return database.Event{}, false, nil
}

// event converts logged event i. Call with s.mu held.
func (s *Store) event(i int) database.Event {
	e := s.Events[i]
	return database.Event{
		SecretKey:     e.SecretKey,
		SecretVersion: e.SecretVersion,
		Kind:          e.Kind,
		Source:        e.Source,
		Detail:        e.Detail,
		OccurredAt:    s.times[i],
	}
}

// EncryptedValue returns the stored (encrypted) value for key, for tests that
// check what actually reaches the database.
func (s *Store) EncryptedValue(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.secrets[key].EncryptedValue
}
