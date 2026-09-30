package vault_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func TestFingerprint(t *testing.T) {
	a, b := encryption.NewCipher("key-a"), encryption.NewCipher("key-b")
	if a.Fingerprint() != encryption.NewCipher("key-a").Fingerprint() {
		t.Error("the same key gave different fingerprints")
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("different keys gave the same fingerprint")
	}
	if len(a.Fingerprint()) != 32 {
		t.Errorf("fingerprint %q isn't 32 hex characters", a.Fingerprint())
	}
}

func TestEnsureKeyRecordsThenRequiresTheSameKey(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	v := vault.New(store, encryption.NewCipher("key-a"))
	_ = v.Create(ctx, "a", "x", "test")

	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := v.KeyStatus(ctx); !st.Recorded || !st.Matches {
		t.Fatalf("after EnsureKey: %+v", st)
	}
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatalf("second start with the same key: %v", err)
	}

	other := vault.New(store, encryption.NewCipher("key-b"))
	if err := other.EnsureKey(ctx); !errors.Is(err, vault.ErrWrongKey) {
		t.Fatalf("start with another key: %v, want ErrWrongKey", err)
	}
}

// Before anything is recorded (the first start after upgrading), a key that
// can't open the stored secrets is refused rather than recorded.
func TestEnsureKeyWontRecordAKeyThatCantDecrypt(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	_ = vault.New(store, encryption.NewCipher("the-real-key")).Create(ctx, "a", "x", "test")

	wrong := vault.New(store, encryption.NewCipher("a-typo"))
	if err := wrong.EnsureKey(ctx); err == nil {
		t.Fatal("a key that can't decrypt the vault was accepted")
	}
	if st, _ := wrong.KeyStatus(ctx); st.Recorded {
		t.Fatal("the wrong key was recorded")
	}
}

// After a rotation by another process, this Cove's writes fail rather than
// store a value encrypted with the old key.
func TestWritesFailAfterTheKeyIsRotated(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	v := vault.New(store, encryption.NewCipher("key-a"))
	_ = v.Create(ctx, "a", "x", "test")
	if err := v.EnsureKey(ctx); err != nil {
		t.Fatal(err)
	}

	store.SetVaultKey(encryption.NewCipher("key-b").Fingerprint())

	if err := v.Create(ctx, "b", "y", "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("Create after rotation: %v", err)
	}
	if _, err := v.Update(ctx, "a", "z", "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("Update after rotation: %v", err)
	}
	if _, err := v.Show(ctx, "b", "test"); !errors.Is(err, vault.ErrNotFound) {
		t.Error("the refused Create was saved")
	}
	if s, _ := v.Show(ctx, "a", "test"); s.Version != 1 {
		t.Error("the refused Update was saved")
	}
}

func TestDecryptFailureWithTheWrongKeySaysSo(t *testing.T) {
	ctx := context.Background()
	store := vaulttest.NewStore()
	a := vault.New(store, encryption.NewCipher("key-a"))
	_ = a.Create(ctx, "a", "x", "test")
	_ = a.EnsureKey(ctx)

	b := vault.New(store, encryption.NewCipher("key-b"))
	if _, err := b.Get(ctx, "a", "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("Get with the wrong key: %v, want ErrWrongKey", err)
	}
	if _, err := b.GetMany(ctx, []string{"a"}, "test"); !errors.Is(err, vault.ErrWrongKey) {
		t.Errorf("GetMany with the wrong key: %v, want ErrWrongKey", err)
	}
}
