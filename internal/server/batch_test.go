package server

import (
	"bytes"
	"log"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/database"
)

// batch sends POST /v0/batch with marquee's token (the setup from
// newProjectAPI: marquee reads marquee.* and shared.tmdb-api-key).
func (a *projectAPI) batch(body string) (int, envelope) {
	a.t.Helper()
	return a.do("POST", "/v0/batch", body, "Authorization", "Bearer "+a.marquee)
}

func batchKeys(t *testing.T, env envelope) []string {
	t.Helper()
	var keys []string
	for _, s := range decode[BatchResponse](t, env).Secrets {
		keys = append(keys, s.Key)
	}
	return keys
}

func TestBatchReturnsEveryKeyInOrder(t *testing.T) {
	api := newProjectAPI(t)

	code, env := api.batch(`{"keys": ["shared.tmdb-api-key", "marquee.db-url", "shared.tmdb-api-key"]}`)
	if code != 200 {
		t.Fatalf("batch = %d %+v", code, env.Error)
	}
	resp := decode[BatchResponse](t, env)
	if len(resp.Secrets) != 2 || resp.Secrets[0].Key != "shared.tmdb-api-key" || resp.Secrets[1].Value != "value of marquee.db-url" || resp.Secrets[1].Version != 1 {
		t.Fatalf("batch = %+v, want both keys in order, duplicates once", resp.Secrets)
	}

	// Each key was counted as a read by marquee.
	var readers []string
	for _, e := range api.store.Events {
		if e.Kind == database.EventRead {
			readers = append(readers, e.SecretKey+"/"+e.Source)
		}
	}
	if !slices.Equal(readers, []string{"shared.tmdb-api-key/marquee", "marquee.db-url/marquee"}) {
		t.Fatalf("read events = %v", readers)
	}
}

func TestBatchRefusesTheWholeRequestWithoutNamingTheKey(t *testing.T) {
	api := newProjectAPI(t)
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	events := len(api.store.Events)

	code, env := api.batch(`{"keys": ["marquee.db-url", "botsuite.db-url", "botsuite.nope"]}`)
	expectError(t, code, env, 403, "forbidden_key")
	if strings.Contains(env.Error.Message, "botsuite") || len(env.Error.Keys) != 0 {
		t.Errorf("the refusal names the forbidden keys: %+v", env.Error)
	}
	if !strings.Contains(logged.String(), "marquee's token can't read botsuite.db-url, botsuite.nope") {
		t.Errorf("the server log should name them for the operator; got %q", logged.String())
	}
	if len(api.store.Events) != events {
		t.Error("a refused batch still read the allowed key")
	}
}

// The forbidden check comes before the existence check, so a mix of a
// forbidden key and a missing one says "forbidden" either way: a project
// can't use a batch to learn whether another project's key exists.
func TestBatchForbiddenBeatsMissing(t *testing.T) {
	api := newProjectAPI(t)
	for _, body := range []string{
		`{"keys": ["marquee.nope", "botsuite.db-url"]}`,
		`{"keys": ["marquee.nope", "botsuite.does-not-exist"]}`,
	} {
		code, env := api.batch(body)
		expectError(t, code, env, 403, "forbidden_key")
	}
}

func TestBatchNamesEveryMissingKeyAndReadsNothing(t *testing.T) {
	api := newProjectAPI(t)
	events := len(api.store.Events)

	code, env := api.batch(`{"keys": ["marquee.db-url", "marquee.one", "shared.tmdb-api-key", "marquee.two"]}`)
	expectError(t, code, env, 404, "not_found")
	if !slices.Equal(env.Error.Keys, []string{"marquee.one", "marquee.two"}) || !strings.Contains(env.Error.Message, "marquee.one, marquee.two") {
		t.Fatalf("error = %+v, want both missing keys named", env.Error)
	}
	if len(api.store.Events) != events {
		t.Error("a batch with missing keys still counted the others as read")
	}
}

func TestBatchWithTheMasterToken(t *testing.T) {
	api := newProjectAPI(t)
	auth := []string{"Authorization", "Bearer " + testToken}

	// Like a single read, the master token must say who's asking.
	code, env := api.do("POST", "/v0/batch", `{"keys": ["botsuite.db-url"]}`, auth...)
	expectError(t, code, env, 400, "missing_source")

	code, env = api.do("POST", "/v0/batch", `{"keys": ["botsuite.db-url", "marquee.db-url"]}`, append(auth, "X-Cove-Source", "old-app")...)
	if code != 200 || !slices.Equal(batchKeys(t, env), []string{"botsuite.db-url", "marquee.db-url"}) {
		t.Fatalf("master token batch = %d %s", code, env.Data)
	}
}

func TestBatchRequestChecks(t *testing.T) {
	api := newProjectAPI(t)

	tooMany := `{"keys": [` + strings.Repeat(`"marquee.x",`, maxBatchKeys) + `"marquee.x"]}`
	for body, wantType := range map[string]string{
		`not json`:     "invalid_body",
		`{"keys": []}`: "invalid_body",
		`{}`:           "invalid_body",
		tooMany:        "invalid_body",
		`{"keys": ["marquee.db-url", "bad key"]}`: "invalid_key",
	} {
		code, env := api.batch(body)
		expectError(t, code, env, 400, wantType)
	}

	code, env := api.do("GET", "/v0/batch", "", "Authorization", "Bearer "+api.marquee)
	expectError(t, code, env, 405, "method_not_allowed")

	code, env = api.do("POST", "/v0/batch", `{"keys": ["marquee.db-url"]}`)
	expectError(t, code, env, 401, "missing_token")
}
