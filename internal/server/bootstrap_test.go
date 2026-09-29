package server

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func TestBootstrapRefusesAddressesOutsideTheAllowedNetworks(t *testing.T) {
	// httptest requests come from 192.0.2.1.
	gate := bootstrap.NewGate(t.TempDir(), []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")})
	if _, err := gate.Open(0); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(nil, gate, &fakePinger{}, Options{ClientSecret: testToken, Port: "0"}).defineRoutes(mux)
	api := &testAPI{t: t, handler: mux, gate: gate}

	code, env := api.do("GET", "/v0/bootstrap/lighthouse", "")
	expectError(t, code, env, 403, "bootstrap_forbidden")

	// The refusal didn't use up the open window.
	if st, _ := gate.Status(); !st.Open {
		t.Fatal("a forbidden request closed the endpoint")
	}
}
