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

func TestSystemRoutesOnlyAcceptGet(t *testing.T) {
	api := newTestAPI(t)
	auth := []string{"Authorization", "Bearer " + testToken}

	for _, path := range []string{"/v0/health", "/v0/ready", "/v0/auth", "/v0/version"} {
		code, env := api.do("POST", path, "", auth...)
		expectError(t, code, env, 405, "method_not_allowed")

		if code, _ := api.do("GET", path, "", auth...); code != 200 {
			t.Errorf("GET %s = %d, want 200", path, code)
		}
	}
}
