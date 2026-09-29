package server

import "testing"

func TestHTTPServerHasTimeouts(t *testing.T) {
	srv := New(nil, nil, testToken, "2100").httpServer()

	if srv.Addr != "0.0.0.0:2100" {
		t.Errorf("Addr = %q", srv.Addr)
	}
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Errorf("missing timeouts: header=%v read=%v write=%v idle=%v",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}
