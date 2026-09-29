package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/tokens/tokenstest"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

func TestBootstrapHandsOutAProjectToken(t *testing.T) {
	ctx := context.Background()
	m := tokens.NewManager(tokenstest.NewStore())
	if _, _, err := m.Create(ctx, "lighthouse", []string{"lighthouse.*"}, nil, "test"); err != nil {
		t.Fatal(err)
	}
	value, err := m.Rotate(ctx, "lighthouse", "test", "for bootstrap")
	if err != nil {
		t.Fatal(err)
	}

	gate := bootstrap.NewGate(t.TempDir(), nil)
	if _, err := gate.OpenFor(0, "lighthouse", value); err != nil {
		t.Fatal(err)
	}

	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("k"))
	mux := http.NewServeMux()
	New(v, gate, &fakePinger{}, Options{ClientSecret: testToken, Tokens: m}).defineRoutes(mux)
	api := &testAPI{t: t, handler: mux, gate: gate}

	code, env := api.do("GET", "/v0/bootstrap/lighthouse", "")
	got := decode[struct {
		Secret string `json:"secret"`
	}](t, env)
	if code != 200 || got.Secret != value {
		t.Fatalf("bootstrap = %d, handed out %q, want lighthouse's token", code, got.Secret)
	}

	// The handed-out token works, and only for lighthouse's keys.
	if code, _ := api.do("GET", "/v0/auth", "", "Authorization", "Bearer "+got.Secret); code != 200 {
		t.Fatalf("the handed-out token doesn't authenticate: %d", code)
	}
	code, env = api.do("GET", "/v0/secrets/marquee.db-url", "", "Authorization", "Bearer "+got.Secret)
	expectError(t, code, env, 403, "forbidden_key")
}
