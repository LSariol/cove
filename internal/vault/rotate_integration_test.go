package vault_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
)

// rotate runs database.RotateKey from one key to another, like
// `cove rotate-key`.
func rotate(ctx context.Context, from, to string) (database.RotationResult, error) {
	oldC, newC := encryption.NewCipher(from), encryption.NewCipher(to)
	return database.RotateKey(ctx, os.Getenv("COVE_TEST_MIGRATE_URL"), oldC.Fingerprint(), newC.Fingerprint(),
		func(v string) (string, error) {
			plain, err := oldC.Decrypt(v)
			if err != nil {
				return "", err
			}
			return newC.Encrypt(plain)
		})
}

const (
	integrationKey = "integration-test-key" // integrationVault's key
	rotatedKey     = "integration-test-key-rotated"
)

func TestIntegrationRotateKey(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}

	key, gone := uniqueKey(t, "rotate"), uniqueKey(t, "rotate-gone")
	for _, step := range []func() error{
		func() error { return v.Create(ctx, key, "v1", "test") },
		func() error { _, err := v.Update(ctx, key, "v2", "test"); return err },
		func() error { return v.Create(ctx, gone, "bye", "test") },
		func() error { return v.Delete(ctx, gone, "test") },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := v.Info(ctx, key)

	result, err := rotate(ctx, integrationKey, rotatedKey)
	if err != nil {
		t.Fatal(err)
	}
	// Put the key back afterwards, for the other tests using this database.
	t.Cleanup(func() {
		if _, err := rotate(ctx, rotatedKey, integrationKey); err != nil {
			t.Errorf("rotate back: %v", err)
		}
	})
	if result.Secrets == 0 || result.EventValues == 0 {
		t.Fatalf("rotated %+v, want secrets and event values", result)
	}

	rotated := vault.New(db, encryption.NewCipher(rotatedKey))
	if err := rotated.EnsureKey(ctx); err != nil {
		t.Fatalf("the new key isn't the vault's key: %v", err)
	}
	if s, err := rotated.Show(ctx, key, "test"); err != nil || s.Value != "v2" {
		t.Fatalf("read with the new key = %+v, %v", s, err)
	}

	// The event log's value copies were re-encrypted too: restore needs them.
	if _, _, err := rotated.Restore(ctx, gone, 0, "test"); err != nil {
		t.Fatalf("restore a deleted secret after rotating: %v", err)
	}
	if s, _ := rotated.Show(ctx, gone, "test"); s.Value != "bye" {
		t.Fatalf("restored value = %q", s.Value)
	}

	// Re-encrypting isn't a modification.
	after, _ := rotated.Info(ctx, key)
	if !after.UpdatedAt.Equal(before.UpdatedAt) || after.Version != before.Version {
		t.Errorf("rotation changed updated_at/version: %v v%d -> %v v%d", before.UpdatedAt, before.Version, after.UpdatedAt, after.Version)
	}

	// A Cove still running with the old key can't write any more.
	if err := v.Create(ctx, uniqueKey(t, "rotate-late"), "x", "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Fatalf("write with the old key after rotating: %v, want ErrWrongKey", err)
	}
	if _, err := v.Show(ctx, key, "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Fatalf("read with the old key after rotating: %v, want ErrWrongKey", err)
	}

	// Rotating again from the old key is refused.
	if _, err := rotate(ctx, integrationKey, rotatedKey); err == nil {
		t.Fatal("rotated from a key the vault no longer uses")
	}
}

// Writes racing a rotation either finish before it (and get re-encrypted) or
// fail after it: no value is ever left encrypted with the old key.
func TestIntegrationRotateKeyDuringWrites(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}
	key := uniqueKey(t, "rotate-race")
	if err := v.Create(ctx, key, "start", "test"); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	done := make(chan []error)
	for w := 0; w < 4; w++ {
		go func() {
			var unexpected []error
			for {
				select {
				case <-stop:
					done <- unexpected
					return
				default:
				}
				if _, err := v.Update(ctx, key, "racing", "test"); err != nil && !errors.Is(err, vault.ErrWrongKey) {
					unexpected = append(unexpected, err)
				}
			}
		}()
	}

	_, err := rotate(ctx, integrationKey, rotatedKey)
	close(stop)
	for w := 0; w < 4; w++ {
		for _, e := range <-done {
			t.Errorf("a racing update failed unexpectedly: %v", e)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := rotate(ctx, rotatedKey, integrationKey); err != nil {
			t.Errorf("rotate back: %v", err)
		}
	})

	// Every stored version of the key opens with the new key.
	newC := encryption.NewCipher(rotatedKey)
	versions, err := db.ValueVersions(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	for ver, value := range versions {
		if _, err := newC.Decrypt(value); err != nil {
			t.Fatalf("version %d of %s is still encrypted with the old key", ver, key)
		}
	}
	current, err := db.GetSecret(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newC.Decrypt(current.EncryptedValue); err != nil {
		t.Fatalf("the current value (v%d) isn't encrypted with the new key", current.Version)
	}
	t.Logf("%d versions written during the race, all readable with the new key", len(versions))
}

// A value that can't be decrypted stops the rotation before anything changes.
func TestIntegrationRotateKeyIsAllOrNothing(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}

	good, bad := uniqueKey(t, "atomic-good"), uniqueKey(t, "atomic-bad")
	if err := v.Create(ctx, good, "fine", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO cove.secrets (key, encrypted_value) VALUES ($1, 'not-a-real-ciphertext')`, bad); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Pool.Exec(ctx, `DELETE FROM cove.secrets WHERE key = $1`, bad) })

	_, err := rotate(ctx, integrationKey, rotatedKey)
	if err == nil || !strings.Contains(err.Error(), bad) || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v, want it to name %s and say nothing changed", err, bad)
	}
	if st, _ := v.KeyStatus(ctx); !st.Matches {
		t.Fatal("the fingerprint changed although the rotation failed")
	}
	if s, err := v.Show(ctx, good, "test"); err != nil || s.Value != "fine" {
		t.Fatalf("a good value after the failed rotation: %+v, %v", s, err)
	}
}

// Only the schema owner may change the recorded key.
func TestIntegrationAppCantChangeTheVaultKey(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE cove.vault_key SET fingerprint = 'x'`, `DELETE FROM cove.vault_key`} {
		if _, err := db.Pool.Exec(ctx, sql); err == nil {
			t.Errorf("cove_app could run %q", sql)
		}
	}
}
