package server

import "testing"

func TestVersionRequiresAuth(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.do("GET", "/v0/version", "")
	expectError(t, code, env, 401, "missing_token")

	code, env = api.do("GET", "/v0/version", "", "Authorization", "Bearer "+testToken)
	if code != 200 || decode[map[string]string](t, env)["version"] != "v9.9.9" {
		t.Fatalf("version = %d %s", code, env.Data)
	}
}
