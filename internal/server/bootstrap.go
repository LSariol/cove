package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func (s *Server) bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}

	if err := s.bootstrap.Lock(); err != nil {
		if errors.Is(err, bootstrap.ErrLocked) {
			log.Printf("bootstrap: refused request from %s (endpoint is locked)", r.RemoteAddr)
			writeError(w, http.StatusForbidden, "bootstrap_locked", "bootstrap has already been completed; open it again with `bootstrap clear` in the Cove CLI")
			return
		}
		log.Printf("bootstrap: couldn't create the marker file: %v", err)
		writeError(w, http.StatusInternalServerError, "marker_error", "the bootstrap marker could not be created")
		return
	}

	if s.clientSecret == "" {
		_ = s.bootstrap.Clear()
		log.Printf("bootstrap: COVE_CLIENT_SECRET is not configured")
		writeError(w, http.StatusInternalServerError, "server_error", "client secret is not configured")
		return
	}

	writeResponse(w, http.StatusOK, struct {
		Secret string `json:"secret"`
	}{Secret: s.clientSecret})

	log.Printf("bootstrap: handed the client token to %s; the endpoint is now locked", r.RemoteAddr)
}
