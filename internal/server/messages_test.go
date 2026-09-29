package server

import (
	"strings"
	"testing"
)

// Error messages can get more specific, but the status and type stay the
// same: that part is the /v0 contract.
func TestErrorMessagesExplainTheCause(t *testing.T) {
	api := newTestAPI(t)
	api.secret("POST", "app.key", `{"value":"one"}`)

	code, env := api.secret("POST", "app.key", `{"value":"two"}`)
	expectError(t, code, env, 500, "create_error")
	if !strings.Contains(env.Error.Message, "already exists") {
		t.Errorf("duplicate create message = %q", env.Error.Message)
	}

	code, env = api.secret("PATCH", "missing.key", `{"value":"x"}`)
	expectError(t, code, env, 500, "update_error")
	if !strings.Contains(env.Error.Message, "no secret with this key") {
		t.Errorf("update of a missing key message = %q", env.Error.Message)
	}
}
