package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
	"github.com/LSariol/Cove/internal/encryption"
	"github.com/LSariol/Cove/internal/tokens"
	"github.com/LSariol/Cove/internal/tokens/tokenstest"
	"github.com/LSariol/Cove/internal/vault"
	"github.com/LSariol/Cove/internal/vault/vaulttest"
)

// projectAPI is a test API with the example setup from the docs: marquee can
// read its own keys and the shared TMDB key, and write its cache keys.
type projectAPI struct {
	*testAPI
	vault   *vault.Vault
	store   *vaulttest.Store
	manager *tokens.Manager
	marquee string // marquee's token
	admin   string // the token of "admin", which may read and change every key
}

func newProjectAPI(t *testing.T) *projectAPI {
	t.Helper()
	ctx := context.Background()

	store := vaulttest.NewStore()
	v := vault.New(store, encryption.NewCipher("test-vault-key"))
	m := tokens.NewManager(tokenstest.NewStore())
	s := New(v, bootstrap.NewGate(t.TempDir(), nil), &fakePinger{}, Options{Port: "0", Tokens: m})

	mux := http.NewServeMux()
	s.defineRoutes(mux)

	for _, key := range []string{"marquee.db-url", "marquee.cache.ttl", "shared.tmdb-api-key", "botsuite.db-url"} {
		if err := v.Create(ctx, key, "value of "+key, "setup"); err != nil {
			t.Fatal(err)
		}
	}
	value, _, err := m.Create(ctx, "marquee", []string{"marquee.*", "shared.tmdb-api-key"}, []string{"marquee.cache.*"}, "setup")
	if err != nil {
		t.Fatal(err)
	}

	admin, _, err := m.Create(ctx, "admin", nil, []string{"*"}, "setup")
	if err != nil {
		t.Fatal(err)
	}

	return &projectAPI{testAPI: &testAPI{t: t, handler: mux}, vault: v, store: store, manager: m, marquee: value, admin: admin}
}

// as sends a request with marquee's token. It also sends an X-Cove-Source
// header naming someone else, as an older client would, to show it's ignored.
func (a *projectAPI) as(method string, key string, body string) (int, envelope) {
	a.t.Helper()
	return a.do(method, "/v0/secrets/"+key, body, "Authorization", "Bearer "+a.marquee, "X-Cove-Source", "pretending")
}

func TestProjectTokenReadsOnlyItsKeys(t *testing.T) {
	api := newProjectAPI(t)

	for _, key := range []string{"marquee.db-url", "shared.tmdb-api-key"} {
		code, env := api.as("GET", key, "")
		if code != 200 || decode[GetSecretResponse](t, env).Value != "value of "+key {
			t.Errorf("GET %s = %d %s", key, code, env.Data)
		}
	}

	code, env := api.as("GET", "botsuite.db-url", "")
	expectError(t, code, env, 403, "forbidden_key")
	if env.Error.Message != "marquee's token can't read botsuite.db-url" {
		t.Errorf("message = %q", env.Error.Message)
	}

	// The same answer whether or not the key exists, so a project can't probe
	// for other projects' keys.
	code, env = api.as("GET", "botsuite.does-not-exist", "")
	expectError(t, code, env, 403, "forbidden_key")

	// A missing key it could read is a plain 404.
	code, env = api.as("GET", "marquee.nope", "")
	expectError(t, code, env, 404, "not_found")
}

func TestProjectTokenWritesOnlyItsWriteKeys(t *testing.T) {
	api := newProjectAPI(t)

	if code, _ := api.as("PATCH", "marquee.cache.ttl", `{"value":"60"}`); code != 200 {
		t.Fatalf("PATCH a write key = %d", code)
	}
	if code, _ := api.as("POST", "marquee.cache.size", `{"value":"10"}`); code != 201 {
		t.Fatalf("POST a write key = %d", code)
	}
	if code, _ := api.as("DELETE", "marquee.cache.size", ""); code != 200 {
		t.Fatalf("DELETE a write key = %d", code)
	}

	for _, req := range [][2]string{{"PATCH", "marquee.db-url"}, {"DELETE", "shared.tmdb-api-key"}, {"POST", "marquee.new"}} {
		code, env := api.as(req[0], req[1], `{"value":"x"}`)
		expectError(t, code, env, 403, "forbidden_key")
	}
	if s, _ := api.vault.Show(context.Background(), "shared.tmdb-api-key", "test"); s.Value != "value of shared.tmdb-api-key" {
		t.Fatal("a refused request changed the secret")
	}
}

func TestProjectTokenIsRecordedByName(t *testing.T) {
	api := newProjectAPI(t)
	api.as("GET", "marquee.db-url", "")

	last := api.store.Events[len(api.store.Events)-1]
	if last.Source != "marquee" {
		t.Fatalf("event source = %q, want the token's name (not the X-Cove-Source header)", last.Source)
	}
}

func TestProjectTokenListSeesOnlyItsKeys(t *testing.T) {
	api := newProjectAPI(t)

	code, env := api.do("GET", "/v0/secrets", "", "Authorization", "Bearer "+api.marquee)
	var keys []string
	for _, s := range decode[SecretSummaryList](t, env).Secrets {
		keys = append(keys, s.Key)
	}
	want := []string{"marquee.cache.ttl", "marquee.db-url", "shared.tmdb-api-key"}
	if code != 200 || len(keys) != len(want) {
		t.Fatalf("list = %d %v, want %v", code, keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("list = %v, want %v", keys, want)
		}
	}

	// A token allowed every key sees them all.
	_, env = api.do("GET", "/v0/secrets", "", "Authorization", "Bearer "+api.admin)
	if n := len(decode[SecretSummaryList](t, env).Secrets); n != 4 {
		t.Fatalf("a * token lists %d secrets, want 4", n)
	}
}

func TestProjectTokenAuthAndRevoke(t *testing.T) {
	api := newProjectAPI(t)
	auth := []string{"Authorization", "Bearer " + api.marquee}

	if code, _ := api.do("GET", "/v0/auth", "", auth...); code != 200 {
		t.Fatalf("/v0/auth with a project token = %d", code)
	}
	if code, _ := api.do("GET", "/v0/version", "", auth...); code != 200 {
		t.Fatalf("/v0/version with a project token = %d", code)
	}

	if err := api.manager.Revoke(context.Background(), "marquee", "test"); err != nil {
		t.Fatal(err)
	}
	code, env := api.do("GET", "/v0/auth", "", auth...)
	expectError(t, code, env, 401, "invalid_token")
}

// failingTokens is a TokenAuthenticator whose database is down.
type failingTokens struct{}

func (failingTokens) Authenticate(context.Context, string) (tokens.Token, error) {
	return tokens.Token{}, errors.New("connection refused")
}

func TestTokenCheckFailureIsNotA401(t *testing.T) {
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("k"))
	s := New(v, bootstrap.NewGate(t.TempDir(), nil), &fakePinger{}, Options{Tokens: failingTokens{}})
	mux := http.NewServeMux()
	s.defineRoutes(mux)
	api := &testAPI{t: t, handler: mux}

	// A client told 401 would think its token is wrong and might discard it.
	code, env := api.do("GET", "/v0/auth", "", "Authorization", "Bearer cove_whatever")
	expectError(t, code, env, 503, "auth_unavailable")
}

func TestEmptyTokenIsRefused(t *testing.T) {
	v := vault.New(vaulttest.NewStore(), encryption.NewCipher("k"))
	s := New(v, bootstrap.NewGate(t.TempDir(), nil), &fakePinger{}, Options{Tokens: tokens.NewManager(tokenstest.NewStore())})
	mux := http.NewServeMux()
	s.defineRoutes(mux)

	req := httptest.NewRequest("GET", "/v0/auth", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("an empty token = %d, want 401", rec.Code)
	}
}
