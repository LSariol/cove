package database_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/tokens"
)

// These tests run against a real, disposable Postgres database, like the ones
// in internal/vault. They're skipped unless COVE_TEST_DATABASE_URL and
// COVE_TEST_MIGRATE_URL are set.
func integrationDB(t *testing.T) *database.Database {
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
	return db
}

// uniqueName keeps repeated runs against the same database independent.
func uniqueName(name string) string {
	return fmt.Sprintf("it-%s-%d", name, os.Getpid())
}

func TestIntegrationTokenLifecycle(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	m := tokens.NewManager(db)
	name := uniqueName("life")

	value, tok, err := m.Create(ctx, name, []string{"it.*", "shared.x"}, []string{"it.cache.*"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tok.Read, []string{"it.*", "shared.x"}) || !slices.Equal(tok.Write, []string{"it.cache.*"}) {
		t.Fatalf("created %+v", tok)
	}
	if _, _, err := m.Create(ctx, name, nil, nil, "test"); !errors.Is(err, tokens.ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}

	got, err := m.Authenticate(ctx, value)
	if err != nil || got.Name != name {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	if got, _ := m.Get(ctx, name); got.LastUsedAt == nil {
		t.Error("last use wasn't recorded")
	}

	already, err := m.Allow(ctx, "shared.y", false, []string{name}, "test")
	if err != nil || len(already) != 0 {
		t.Fatalf("Allow = %v, %v", already, err)
	}
	if already, _ := m.Allow(ctx, "shared.y", false, []string{name}, "test"); len(already) != 1 {
		t.Fatal("allowing twice added the pattern twice")
	}
	if _, err := m.Allow(ctx, "shared.z", false, []string{name, uniqueName("missing")}, "test"); !errors.Is(err, tokens.ErrNotFound) {
		t.Fatalf("Allow with an unknown name: %v", err)
	}
	if got, _ := m.Get(ctx, name); got.CanRead("shared.z") {
		t.Fatal("a failed Allow was partly applied")
	}

	if notListed, err := m.Deny(ctx, "shared.x", []string{name}, "test"); err != nil || len(notListed) != 0 {
		t.Fatalf("Deny = %v, %v", notListed, err)
	}

	renamed, err := m.RenameKey(ctx, "shared.y", "shared.y2", "test")
	if err != nil || !slices.Contains(renamed, name) {
		t.Fatalf("RenameKey = %v, %v", renamed, err)
	}
	got, _ = m.Get(ctx, name)
	if !slices.Equal(got.Read, []string{"it.*", "shared.y2"}) {
		t.Fatalf("read patterns = %v", got.Read)
	}

	newValue, err := m.Rotate(ctx, name, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, value); !errors.Is(err, tokens.ErrNotFound) {
		t.Fatal("the old value still works after rotating")
	}
	if _, err := m.Authenticate(ctx, newValue); err != nil {
		t.Fatal(err)
	}

	if err := m.Revoke(ctx, name, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, newValue); !errors.Is(err, tokens.ErrNotFound) {
		t.Fatal("a revoked token still works")
	}

	events, err := m.History(ctx, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	want := []string{"revoke", "rotate", "rename_key", "deny", "allow", "create"}
	if !slices.Equal(actions, want) {
		t.Fatalf("history = %v, want %v", actions, want)
	}
}

func TestIntegrationTokenLogIsAppendOnly(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	if _, err := db.Pool.Exec(ctx, `DELETE FROM cove.token_log`); err == nil {
		t.Fatal("cove_app deleted token_log rows")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE cove.token_log SET detail = 'x'`); err == nil {
		t.Fatal("cove_app changed token_log rows")
	}
}

func TestIntegrationTokenNameCheck(t *testing.T) {
	db := integrationDB(t)
	hash := tokens.Hash("cove_check")
	if _, err := db.CreateToken(context.Background(), "Not Valid", hash, nil, nil); err == nil {
		t.Fatal("the database accepted an invalid token name")
	}
}
