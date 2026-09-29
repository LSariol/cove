package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
)

type fakePinger struct {
	err error
}

func (p *fakePinger) Ping(ctx context.Context) error {
	return p.err
}

func TestReadyReflectsDatabase(t *testing.T) {
	db := &fakePinger{}
	mux := http.NewServeMux()
	New(nil, bootstrap.NewGate(t.TempDir(), nil), db, Options{ClientSecret: testToken, Port: "0"}).defineRoutes(mux)
	api := &testAPI{t: t, handler: mux}

	code, env := api.do("GET", "/v0/ready", "")
	if code != 200 || decode[map[string]any](t, env)["ready"] != true {
		t.Fatalf("ready with a reachable database = %d %s", code, env.Data)
	}

	db.err = errors.New("connection refused")
	code, env = api.do("GET", "/v0/ready", "")
	expectError(t, code, env, 503, "not_ready")

	// /v0/health stays up regardless: it only reports the HTTP server.
	if code, _ := api.do("GET", "/v0/health", ""); code != 200 {
		t.Fatalf("health with the database down = %d, want 200", code)
	}
}
