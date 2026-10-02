package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

// These tests pin down the /v0 API contract that CoveClient and every project
// depend on: status codes, error types, and JSON field names.

// testToken is the token of "test", a project that may read and change every
// key.
const testToken = "cove_test-token-0123456789"

// fullAccess is a TokenAuthenticator that knows only testToken.
type fullAccess struct{}

func (fullAccess) Authenticate(_ context.Context, value string) (tokens.Token, error) {
	if value != testToken {
		return tokens.Token{}, tokens.ErrNotFound
	}
	return tokens.Token{Name: "test", Write: []string{"*"}}, nil
}

type testAPI struct {
	t       *testing.T
	handler http.Handler
	gate    *bootstrap.Gate
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("test-vault-key"))
	gate := bootstrap.NewGate(t.TempDir(), nil)
	s := New(v, gate, &fakePinger{}, Options{Tokens: fullAccess{}, Port: "0", Version: "v9.9.9"})

	mux := http.NewServeMux()
	s.defineRoutes(mux)
	return &testAPI{t: t, handler: mux, gate: gate}
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *APIError       `json:"error"`
}

// do sends a request. headers alternate name, value.
func (a *testAPI) do(method string, path string, body string, headers ...string) (int, envelope) {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)

	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		a.t.Fatalf("%s %s: response is not a JSON envelope: %q", method, path, rec.Body.String())
	}
	return rec.Code, env
}

// secret sends a request with testToken.
func (a *testAPI) secret(method string, key string, body string) (int, envelope) {
	a.t.Helper()
	return a.do(method, "/v0/secrets/"+key, body, "Authorization", "Bearer "+testToken)
}

func expectError(t *testing.T, code int, env envelope, wantCode int, wantType string) {
	t.Helper()
	if code != wantCode || env.Success || env.Error == nil || env.Error.Type != wantType {
		t.Fatalf("got %d %+v, want %d %s", code, env.Error, wantCode, wantType)
	}
}

func decode[T any](t *testing.T, env envelope) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(env.Data, &v); err != nil {
		t.Fatalf("decode data %s: %v", env.Data, err)
	}
	return v
}

func TestHealthNeedsNoAuth(t *testing.T) {
	api := newTestAPI(t)
	code, env := api.do("GET", "/v0/health", "")

	data := decode[map[string]any](t, env)
	if code != 200 || !env.Success || data["healthy"] != true {
		t.Fatalf("health = %d %s", code, env.Data)
	}
}

func TestAuthentication(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.do("GET", "/v0/auth", "")
	expectError(t, code, env, 401, "missing_token")

	code, env = api.do("GET", "/v0/auth", "", "Authorization", "Token "+testToken)
	expectError(t, code, env, 401, "invalid_token_format")

	code, env = api.do("GET", "/v0/auth", "", "Authorization", "Bearer wrong")
	expectError(t, code, env, 401, "invalid_token")

	code, env = api.do("GET", "/v0/auth", "", "Authorization", "Bearer "+testToken)
	if code != 200 || decode[map[string]any](t, env)["authenticated"] != true {
		t.Fatalf("auth with a valid token = %d %s", code, env.Data)
	}
}

func TestSecretRequestChecks(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.secret("GET", "bad:key", "")
	expectError(t, code, env, 400, "invalid_key")

	code, env = api.secret("PUT", "app.key", `{"value":"x"}`)
	expectError(t, code, env, 405, "method_not_allowed")

	code, env = api.secret("POST", "app.key", `not json`)
	expectError(t, code, env, 400, "invalid_body")

	code, env = api.secret("POST", "app.key", `{"value":"`+strings.Repeat("x", maxBodyBytes)+`"}`)
	expectError(t, code, env, 400, "invalid_body")
}

func TestSecretLifecycle(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.secret("POST", "app.key", `{"value":"one"}`)
	created := decode[SecretActionResponse](t, env)
	if code != 201 || created.Key != "app.key" || created.Action != "created" {
		t.Fatalf("create = %d %s", code, env.Data)
	}

	code, env = api.secret("GET", "app.key", "")
	got := decode[GetSecretResponse](t, env)
	if code != 200 || got.Key != "app.key" || got.Value != "one" || got.Version != 1 {
		t.Fatalf("get = %d %s", code, env.Data)
	}

	code, env = api.secret("PATCH", "app.key", `{"value":"two"}`)
	if code != 200 || decode[SecretActionResponse](t, env).Action != "updated" {
		t.Fatalf("update = %d %s", code, env.Data)
	}

	_, env = api.secret("GET", "app.key", "")
	if got := decode[GetSecretResponse](t, env); got.Value != "two" || got.Version != 2 {
		t.Fatalf("get after update = %s", env.Data)
	}

	code, env = api.secret("DELETE", "app.key", "")
	if code != 200 || decode[SecretActionResponse](t, env).Action != "deleted" {
		t.Fatalf("delete = %d %s", code, env.Data)
	}

	code, env = api.secret("GET", "app.key", "")
	expectError(t, code, env, 404, "not_found")

	code, env = api.secret("DELETE", "app.key", "")
	expectError(t, code, env, 404, "not_found")
}

func TestListReturnsMetadataOnly(t *testing.T) {
	api := newTestAPI(t)
	api.secret("POST", "b.key", `{"value":"secret-b"}`)
	api.secret("POST", "a.key", `{"value":"secret-a"}`)
	api.secret("GET", "a.key", "")

	code, env := api.do("GET", "/v0/secrets", "", "Authorization", "Bearer "+testToken)
	if code != 200 {
		t.Fatalf("list = %d", code)
	}
	if strings.Contains(string(env.Data), "secret-a") || strings.Contains(string(env.Data), "secret-b") {
		t.Fatal("list response contains secret values")
	}

	list := decode[struct {
		Secrets []map[string]any `json:"secrets"`
	}](t, env)
	if len(list.Secrets) != 2 || list.Secrets[0]["key"] != "a.key" {
		t.Fatalf("list = %s", env.Data)
	}
	for _, field := range []string{"key", "version", "times_pulled", "created_at", "updated_at"} {
		if _, ok := list.Secrets[0][field]; !ok {
			t.Errorf("list entry is missing %q", field)
		}
	}
	if list.Secrets[0]["times_pulled"] != float64(1) {
		t.Errorf("times_pulled = %v, want 1", list.Secrets[0]["times_pulled"])
	}
}

// handoutToken is the project token the bootstrap tests open the endpoint with.
const handoutToken = "cove_lighthouse-token-for-tests"

func TestBootstrapIsClosedUntilOpened(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.do("GET", "/v0/bootstrap/lighthouse", "")
	expectError(t, code, env, 403, "bootstrap_locked")

	if _, err := api.gate.OpenFor(0, "lighthouse", handoutToken); err != nil {
		t.Fatal(err)
	}
	code, env = api.do("GET", "/v0/bootstrap/lighthouse", "")
	if code != 200 || decode[map[string]string](t, env)["secret"] != handoutToken {
		t.Fatalf("bootstrap while open = %d %s", code, env.Data)
	}

	// httptest requests all come from the same address, so a second request
	// falls in the grace period; the gate's own tests cover other addresses.
	if err := api.gate.Lock(); err != nil {
		t.Fatal(err)
	}
	code, env = api.do("GET", "/v0/bootstrap/lighthouse", "")
	expectError(t, code, env, 403, "bootstrap_locked")
}
