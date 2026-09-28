package server

import (
	"fmt"
	"net/http"
)

func (s *Server) bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}

	if err := s.bootstrap.Lock(); err != nil {
		writeError(w, http.StatusForbidden, "bootstrap_locked", "bootstrap has already been completed")
		return
	}

	if s.clientSecret == "" {
		_ = s.bootstrap.Clear()
		writeError(w, http.StatusInternalServerError, "server_error", "client secret is not configured")
		return
	}

	writeResponse(w, http.StatusOK, struct {
		Secret string `json:"secret"`
	}{Secret: s.clientSecret})

	fmt.Println("Bootstrap complete")
}
