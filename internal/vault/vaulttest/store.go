// Package vaulttest provides an in-memory vault.Store for tests, so code built
// on a Vault can be tested without Postgres.
package vaulttest

import (
	"context"
	"fmt"
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

	// Events holds every logged event, in order.
	Events []database.EventLogInput
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

func (s *Store) LogEvent(ctx context.Context, logInfo database.EventLogInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Events = append(s.Events, logInfo)
	return nil
}

// EncryptedValue returns the stored (encrypted) value for key, for tests that
// check what actually reaches the database.
func (s *Store) EncryptedValue(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.secrets[key].EncryptedValue
}
