package vault_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LSariol/Cove/internal/database"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func newManyVault(t *testing.T) (*vault.Vault, *vaulttest.Store) {
	t.Helper()
	store := vaulttest.NewStore()
	v := vault.New(store, encryption.NewCipher("test-vault-key"))
	for _, key := range []string{"a", "b", "c"} {
		if err := v.Create(context.Background(), key, "value of "+key, "setup"); err != nil {
			t.Fatal(err)
		}
	}
	return v, store
}

func reads(store *vaulttest.Store) int {
	n := 0
	for _, e := range store.Events {
		if e.Kind == database.EventRead {
			n++
		}
	}
	return n
}

func TestGetManyReturnsInOrderAndCountsEach(t *testing.T) {
	ctx := context.Background()
	v, store := newManyVault(t)

	got, err := v.GetMany(ctx, []string{"c", "a", "c"}, "lighthouse")
	if err != nil {
		t.Fatal(err)
	}
	var keys, values []string
	for _, s := range got {
		keys, values = append(keys, s.Key), append(values, s.Value)
	}
	if !slices.Equal(keys, []string{"c", "a"}) || !slices.Equal(values, []string{"value of c", "value of a"}) {
		t.Fatalf("got %v %v, want c, a in order with duplicates removed", keys, values)
	}

	if n := reads(store); n != 2 {
		t.Errorf("%d read events, want 2", n)
	}
	if info, _ := v.Info(ctx, "c"); info.ReadCount != 1 {
		t.Errorf("read_count of c = %d, want 1", info.ReadCount)
	}
	for _, e := range store.Events {
		if e.Kind == database.EventRead && e.Source != "lighthouse" {
			t.Errorf("read logged with source %q", e.Source)
		}
	}
}

func TestGetManyNamesEveryMissingKeyAndReadsNothing(t *testing.T) {
	ctx := context.Background()
	v, store := newManyVault(t)

	_, err := v.GetMany(ctx, []string{"a", "x", "b", "y"}, "lighthouse")
	var missing *vault.MissingError
	if !errors.As(err, &missing) || !slices.Equal(missing.Keys, []string{"x", "y"}) {
		t.Fatalf("err = %v, want MissingError{x, y}", err)
	}
	if !errors.Is(err, vault.ErrNotFound) {
		t.Error("MissingError doesn't match ErrNotFound")
	}

	// All or nothing: a and b weren't counted as read.
	if n := reads(store); n != 0 {
		t.Errorf("%d read events after a failed batch, want 0", n)
	}
	if info, _ := v.Info(ctx, "a"); info.ReadCount != 0 {
		t.Errorf("read_count of a = %d after a failed batch, want 0", info.ReadCount)
	}
}

func TestGetManyFailsOnADecryptError(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	if err := vault.New(store, encryption.NewCipher("old-key")).Create(ctx, "a", "x", "setup"); err != nil {
		t.Fatal(err)
	}
	v := vault.New(store, encryption.NewCipher("new-key"))

	if _, err := v.GetMany(ctx, []string{"a"}, "lighthouse"); !errors.Is(err, vault.ErrDecrypt) {
		t.Fatalf("err = %v, want ErrDecrypt", err)
	}
	if n := reads(store); n != 0 {
		t.Errorf("%d read events after a decrypt failure", n)
	}
}
