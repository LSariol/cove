package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

type fakeDB struct {
	pingErr    error
	have, want int64
}

func (f *fakeDB) Ping(ctx context.Context) error { return f.pingErr }
func (f *fakeDB) SchemaVersion(ctx context.Context) (int64, int64, error) {
	return f.have, f.want, nil
}

func newStatusCLI(t *testing.T, db *fakeDB) *CLI {
	t.Helper()
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("test-vault-key"))
	_ = v.Create(context.Background(), "a.key", "x", "test")
	return New(v, bootstrap.NewGate(t.TempDir(), nil), Options{Env: "PROD", Version: "v1.0.0", DB: db})
}

func TestStatusHealthy(t *testing.T) {
	c := newStatusCLI(t, &fakeDB{have: 6, want: 6})
	o, _ := captureOutput(t)

	if err := c.Exec(context.Background(), []string{"status"}); err != nil {
		t.Fatalf("healthy status returned %v", err)
	}
	for _, want := range []string{"v1.0.0", "prod", "reachable", "version 6 (up to date)", "Secrets:", "1", "closed"} {
		if !strings.Contains(o.String(), want) {
			t.Errorf("status is missing %q:\n%s", want, o.String())
		}
	}
}

func TestStatusReportsProblems(t *testing.T) {
	c := newStatusCLI(t, &fakeDB{have: 5, want: 6})
	captureOutput(t)
	if err := c.Exec(context.Background(), []string{"status"}); err == nil || !strings.Contains(err.Error(), "migrations are missing") {
		t.Errorf("status with an old schema = %v", err)
	}

	c = newStatusCLI(t, &fakeDB{pingErr: errors.New("refused")})
	o, _ := captureOutput(t)
	if err := c.Exec(context.Background(), []string{"status"}); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("status with the database down = %v", err)
	}
	if !strings.Contains(o.String(), "unreachable") {
		t.Errorf("status output = %s", o.String())
	}
}
