package cli

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func newTestCLI(t *testing.T, input string) (*CLI, *vault.Vault) {
	t.Helper()
	captureOutput(t)
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("test-vault-key"))
	c := New(v, nil, Options{})
	c.scanner = bufio.NewScanner(strings.NewReader(input))
	return c, v
}

func exists(v *vault.Vault, key string) bool {
	_, err := v.Show(context.Background(), key, "test")
	return err == nil
}

func TestDeleteAsksFirst(t *testing.T) {
	ctx := context.Background()

	c, v := newTestCLI(t, "n\n")
	_ = v.Create(ctx, "a.key", "x", "test")
	if err := c.Exec(ctx, []string{"delete", "a.key"}); err != nil || !exists(v, "a.key") {
		t.Fatalf("answering n: err=%v, still exists=%v", err, exists(v, "a.key"))
	}

	c, v = newTestCLI(t, "y\n")
	_ = v.Create(ctx, "a.key", "x", "test")
	if err := c.Exec(ctx, []string{"delete", "a.key"}); err != nil || exists(v, "a.key") {
		t.Fatalf("answering y: err=%v, still exists=%v", err, exists(v, "a.key"))
	}
}

func TestDeleteYesSkipsTheQuestion(t *testing.T) {
	ctx := context.Background()

	for _, flag := range []string{"--yes", "-y"} {
		c, v := newTestCLI(t, "") // no input available at all
		_ = v.Create(ctx, "a.key", "x", "test")
		if err := c.Exec(ctx, []string{"delete", flag, "a.key"}); err != nil || exists(v, "a.key") {
			t.Fatalf("delete %s: err=%v, still exists=%v", flag, err, exists(v, "a.key"))
		}
	}
}

func TestDeleteWithoutAnswerExplainsYes(t *testing.T) {
	ctx := context.Background()
	c, v := newTestCLI(t, "")
	_ = v.Create(ctx, "a.key", "x", "test")

	err := c.Exec(ctx, []string{"delete", "a.key"})
	if err == nil || !strings.Contains(err.Error(), "--yes") || !exists(v, "a.key") {
		t.Fatalf("delete with no input: err=%v, still exists=%v", err, exists(v, "a.key"))
	}
}
