package server

import (
	"strings"
	"testing"
)

func TestEmptyListIsAnArray(t *testing.T) {
	api := newTestAPI(t)

	code, env := api.do("GET", "/v0/secrets", "", "Authorization", "Bearer "+testToken)
	if code != 200 {
		t.Fatalf("list = %d", code)
	}
	if got := strings.ReplaceAll(string(env.Data), " ", ""); got != `{"secrets":[]}` {
		t.Fatalf("empty list data = %s, want {\"secrets\":[]}", env.Data)
	}
}
