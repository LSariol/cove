package vault_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/jackc/pgx/v5"
)

// These tests run against a real, disposable Postgres database. They're
// skipped unless both variables are set, e.g.:
//
//	COVE_TEST_MIGRATE_URL=postgres://cove_migrator:...@localhost:5499/cove_db
//	COVE_TEST_DATABASE_URL=postgres://cove_app:...@localhost:5499/cove_db
//
// They create and change secrets, so never point them at a real vault.
func integrationVault(t *testing.T) (*vault.Vault, *database.Database) {
	t.Helper()
	appURL, migrateURL := os.Getenv("COVE_TEST_DATABASE_URL"), os.Getenv("COVE_TEST_MIGRATE_URL")
	if appURL == "" || migrateURL == "" {
		t.Skip("set COVE_TEST_DATABASE_URL and COVE_TEST_MIGRATE_URL to run integration tests")
	}

	ctx := context.Background()
	if err := database.Migrate(ctx, migrateURL); err != nil {
		t.Fatal(err)
	}

	db := database.New(appURL)
	if err := db.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	return vault.New(db, encryption.NewCipher("integration-test-key")), db
}

// uniqueKey keeps repeated runs against the same database independent.
func uniqueKey(t *testing.T, name string) string {
	return fmt.Sprintf("it.%s.%d", name, os.Getpid())
}

func TestIntegrationConcurrentUpdatesKeepAConsistentHistory(t *testing.T) {
	v, _ := integrationVault(t)
	ctx := context.Background()
	key := uniqueKey(t, "concurrent")

	if err := v.Create(ctx, key, "v1", "test"); err != nil {
		t.Fatal(err)
	}

	const updates = 20
	var wg sync.WaitGroup
	for i := range updates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := v.Update(ctx, key, fmt.Sprintf("value-%d", i), "test"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got, err := v.Show(ctx, key, "test")
	if err != nil || got.Version != updates+1 {
		t.Fatalf("after %d concurrent updates: version %d, %v", updates, got.Version, err)
	}

	// Every update event's version is unique and consecutive: none were lost
	// or recorded against the wrong previous value.
	events, err := v.History(ctx, key, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, e := range events {
		if e.Kind == database.EventUpdate {
			if seen[e.SecretVersion] {
				t.Errorf("version %d was recorded twice", e.SecretVersion)
			}
			seen[e.SecretVersion] = true
		}
	}
	for version := 2; version <= updates+1; version++ {
		if !seen[version] {
			t.Errorf("no update event for version %d", version)
		}
	}
}

func TestIntegrationUnrecordedChangesAreRolledBack(t *testing.T) {
	v, _ := integrationVault(t)
	ctx := context.Background()
	key := uniqueKey(t, "rollback")

	// Take away cove_app's right to write the event log for a moment.
	owner, err := pgx.Connect(ctx, os.Getenv("COVE_TEST_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	if _, err := owner.Exec(ctx, `REVOKE INSERT ON cove.event_log FROM cove_app`); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if !restored {
			restored = true
			if _, err := owner.Exec(ctx, `GRANT INSERT ON cove.event_log TO cove_app`); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer restore()

	if err := v.Create(ctx, key, "x", "test"); err == nil {
		t.Fatal("Create succeeded without being able to record it")
	}
	restore()

	if _, err := v.Show(ctx, key, "test"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("the unrecorded create is still there: %v", err)
	}
}

func TestIntegrationFailedDecryptIsNotCounted(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	key := uniqueKey(t, "decrypt")

	if err := v.Create(ctx, key, "x", "test"); err != nil {
		t.Fatal(err)
	}

	wrongKey := vault.New(db, encryption.NewCipher("some-other-key"))
	if _, err := wrongKey.Get(ctx, key, "test"); !errors.Is(err, vault.ErrDecrypt) {
		t.Fatalf("Get = %v, want ErrDecrypt", err)
	}

	info, err := v.Info(ctx, key)
	if err != nil || info.ReadCount != 0 {
		t.Fatalf("read count after a failed decrypt = %d, %v", info.ReadCount, err)
	}
}

func TestIntegrationPruneReadEvents(t *testing.T) {
	v, db := integrationVault(t)
	ctx := context.Background()
	key := uniqueKey(t, "prune")

	if err := v.Create(ctx, key, "x", "test"); err != nil {
		t.Fatal(err)
	}
	// An old read, and a recent one.
	for _, age := range []string{"100 days", "1 hour"} {
		_, err := db.Pool.Exec(ctx, `INSERT INTO cove.event_log (secret_key, secret_version, kind, source, occurred_at)
			VALUES ($1, 1, 'read', 'test', now() - $2::interval)`, key, age)
		if err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.PruneReadEvents(ctx, 90); err != nil {
		t.Fatal(err)
	}

	events, _ := v.History(ctx, key, 0)
	kinds := map[database.EventKind]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	if kinds[database.EventRead] != 1 || kinds[database.EventCreate] != 1 {
		t.Fatalf("after pruning: %v, want the recent read and the create kept", kinds)
	}

	// The app still can't delete log rows directly.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM cove.event_log WHERE secret_key = $1`, key); err == nil {
		t.Fatal("cove_app deleted event log rows directly")
	}
}

func TestIntegrationRestoreAfterRename(t *testing.T) {
	v, _ := integrationVault(t)
	ctx := context.Background()
	oldKey, newKey := uniqueKey(t, "before-rename"), uniqueKey(t, "AFTER_RENAME")

	if err := v.Create(ctx, oldKey, "first", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Update(ctx, oldKey, "second", "test"); err != nil {
		t.Fatal(err)
	}
	if err := v.Rename(ctx, oldKey, newKey, "test"); err != nil {
		t.Fatal(err)
	}

	if _, from, err := v.Restore(ctx, newKey, 1, "test"); err != nil || from != 1 {
		t.Fatalf("restore version 1 after rename: from %d, %v", from, err)
	}
	if s, _ := v.Show(ctx, newKey, "test"); s.Value != "first" {
		t.Fatalf("value = %q, want the one from before the rename", s.Value)
	}
}
