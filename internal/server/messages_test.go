package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

// Error status codes say what went wrong. CoveClient only checks the success
// codes (200/201), so changing these doesn't affect it.
func TestErrorStatusCodes(t *testing.T) {
	api := newTestAPI(t)
	api.secret("POST", "app.key", `{"value":"one"}`)

	code, env := api.secret("POST", "app.key", `{"value":"two"}`)
	expectError(t, code, env, 409, "already_exists")
	if !strings.Contains(env.Error.Message, "PATCH") {
		t.Errorf("duplicate create message = %q", env.Error.Message)
	}

	code, env = api.secret("PATCH", "missing.key", `{"value":"x"}`)
	expectError(t, code, env, 404, "not_found")
	if !strings.Contains(env.Error.Message, "POST") {
		t.Errorf("update of a missing key message = %q", env.Error.Message)
	}

	code, env = api.secret("GET", "missing.key", "")
	expectError(t, code, env, 404, "not_found")
	code, env = api.secret("DELETE", "missing.key", "")
	expectError(t, code, env, 404, "not_found")
}

func TestUndecryptableSecretIsAServerError(t *testing.T) {
	store := vaulttest.NewStore()
	_ = vault.New(store, encryption.NewCipher("the-old-key")).Create(t.Context(), "app.key", "x", "test")

	// The same data read with a different vault key.
	v := vault.New(store, encryption.NewCipher("a-new-key"))
	mux := http.NewServeMux()
	New(v, bootstrap.NewGate(t.TempDir(), nil), &fakePinger{}, Options{Tokens: fullAccess{}, Port: "0"}).defineRoutes(mux)
	api := &testAPI{t: t, handler: mux}

	code, env := api.secret("GET", "app.key", "")
	expectError(t, code, env, 500, "decrypt_error")
}
