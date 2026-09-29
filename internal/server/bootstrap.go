package server

import (
	"log"
	"net/http"
	"net/netip"

	"github.com/LSariol/Cove/internal/bootstrap"
)

// bootstrapHandler hands the client token to a new client, when the bootstrap
// gate allows it (see package bootstrap).
func (s *Server) bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}

	if s.clientSecret == "" {
		log.Printf("bootstrap: COVE_CLIENT_SECRET is not configured")
		writeError(w, http.StatusInternalServerError, "server_error", "client secret is not configured")
		return
	}

	addr := remoteAddr(r)

	outcome, err := s.bootstrap.Claim(addr)
	if err != nil {
		log.Printf("bootstrap: request from %s failed: %v", addr, err)
		writeError(w, http.StatusInternalServerError, "marker_error", "the bootstrap state could not be read or saved")
		return
	}

	switch outcome {
	case bootstrap.Granted:
		log.Printf("bootstrap: handed the client token to %s; the endpoint is now closed", addr)
	case bootstrap.Redelivered:
		log.Printf("bootstrap: handed the client token to %s again (within the grace period)", addr)
	case bootstrap.Expired:
		log.Printf("bootstrap: refused %s (the window expired)", addr)
		writeError(w, http.StatusForbidden, "bootstrap_expired", "the bootstrap window expired before it was used; open it again with `bootstrap open` in the Cove CLI")
		return
	case bootstrap.Forbidden:
		log.Printf("bootstrap: refused %s (not in COVE_BOOTSTRAP_ALLOWED_CIDRS)", addr)
		writeError(w, http.StatusForbidden, "bootstrap_forbidden", "this address isn't allowed to bootstrap")
		return
	default:
		log.Printf("bootstrap: refused %s (the endpoint is closed)", addr)
		writeError(w, http.StatusForbidden, "bootstrap_locked", "the bootstrap endpoint is closed; open it with `bootstrap open` in the Cove CLI")
		return
	}

	writeResponse(w, http.StatusOK, struct {
		Secret string `json:"secret"`
	}{Secret: s.clientSecret})
}

// remoteAddr returns the address of the connection the request came from. It
// deliberately ignores headers such as X-Forwarded-For, which a client could
// set to anything.
func remoteAddr(r *http.Request) netip.Addr {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return addrPort.Addr().Unmap()
}
