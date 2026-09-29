package vault_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func newVault(t *testing.T) (*vault.Vault, *vaulttest.Store) {
	t.Helper()
	store := vaulttest.NewStore()
	return vault.New(store, encryption.NewCipher("test-vault-key")), store
}

func TestCreateStoresEncryptedValueAndLogs(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)

	if err := v.Create(ctx, "app.key", "plaintext", "test"); err != nil {
		t.Fatal(err)
	}

	if got := store.EncryptedValue("app.key"); got == "" || got == "plaintext" {
		t.Fatalf("stored value = %q, want ciphertext", got)
	}

	if len(store.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.Events))
	}
	e := store.Events[0]
	if e.Kind != database.EventCreate || e.Source != "test" || e.OldEncryptedValue != nil || e.NewEncryptedValue == nil {
		t.Fatalf("unexpected create event: %+v", e)
	}
}

func TestGetDecryptsAndLogsRead(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)
	_ = v.Create(ctx, "app.key", "plaintext", "test")

	got, err := v.Get(ctx, "app.key", "reader")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "plaintext" || got.Version != 1 {
		t.Fatalf("Get = %+v", got)
	}

	last := store.Events[len(store.Events)-1]
	if last.Kind != database.EventRead || last.Source != "reader" {
		t.Fatalf("unexpected read event: %+v", last)
	}
}

func TestUpdateBumpsVersionAndLogsOldAndNew(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)
	_ = v.Create(ctx, "app.key", "one", "test")
	before := store.EncryptedValue("app.key")

	if err := v.Update(ctx, "app.key", "two", "test"); err != nil {
		t.Fatal(err)
	}

	got, _ := v.Get(ctx, "app.key", "test")
	if got.Value != "two" || got.Version != 2 {
		t.Fatalf("after update = %+v", got)
	}

	update := store.Events[1]
	if update.Kind != database.EventUpdate || *update.OldEncryptedValue != before || update.SecretVersion != 2 {
		t.Fatalf("unexpected update event: %+v", update)
	}
}

func TestDeleteLogsLastValue(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)
	_ = v.Create(ctx, "app.key", "one", "test")
	stored := store.EncryptedValue("app.key")

	if err := v.Delete(ctx, "app.key", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "app.key", "test"); err == nil {
		t.Fatal("Get after Delete succeeded")
	}

	del := store.Events[1]
	if del.Kind != database.EventDelete || *del.OldEncryptedValue != stored {
		t.Fatalf("unexpected delete event: %+v", del)
	}
}

func TestListOmitsValues(t *testing.T) {
	ctx := context.Background()
	v, _ := newVault(t)
	_ = v.Create(ctx, "b.key", "two", "test")
	_ = v.Create(ctx, "a.key", "one", "test")

	secrets, err := v.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 2 || secrets[0].Key != "a.key" || secrets[1].Key != "b.key" {
		t.Fatalf("List = %+v", secrets)
	}
	for _, s := range secrets {
		if s.Value != "" {
			t.Fatalf("List returned a value for %q", s.Key)
		}
	}
}

func TestMissingSecretErrors(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)

	if _, err := v.Get(ctx, "missing", "test"); err == nil {
		t.Error("Get of a missing key succeeded")
	}
	if err := v.Update(ctx, "missing", "x", "test"); err == nil {
		t.Error("Update of a missing key succeeded")
	}
	if err := v.Delete(ctx, "missing", "test"); err == nil {
		t.Error("Delete of a missing key succeeded")
	}
	if len(store.Events) != 0 {
		t.Errorf("failed operations logged %d events, want 0", len(store.Events))
	}
}

func TestCreateRejectsInvalidKeys(t *testing.T) {
	ctx := context.Background()
	v, store := newVault(t)

	for _, key := range []string{"github/token", "db:url", "has space", ""} {
		if err := v.Create(ctx, key, "value", "cove_cli"); err == nil {
			t.Errorf("Create(%q) succeeded, want an error", key)
		}
	}
	if len(store.Events) != 0 {
		t.Errorf("rejected creates logged %d events", len(store.Events))
	}
}

func TestErrorKinds(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	v := vault.New(store, encryption.NewCipher("test-vault-key"))

	if _, err := v.Get(ctx, "missing", "test"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
	if err := v.Update(ctx, "missing", "x", "test"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
	if err := v.Delete(ctx, "missing", "test"); !errors.Is(err, vault.ErrNotFound) {
		t.Errorf("Delete missing = %v, want ErrNotFound", err)
	}

	_ = v.Create(ctx, "app.key", "one", "test")
	if err := v.Create(ctx, "app.key", "two", "test"); !errors.Is(err, vault.ErrAlreadyExists) {
		t.Errorf("duplicate Create = %v, want ErrAlreadyExists", err)
	}

	otherKey := vault.New(store, encryption.NewCipher("a-different-key"))
	if _, err := otherKey.Get(ctx, "app.key", "test"); !errors.Is(err, vault.ErrDecrypt) {
		t.Errorf("Get with the wrong vault key = %v, want ErrDecrypt", err)
	}
}
