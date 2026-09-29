package server

import (
	"errors"
	"fmt"
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
			writeError(w, http.StatusForbidden, "bootstrap_locked", "bootstrap has already been completed")
			return
		}
		log.Printf("bootstrap: %v", err)
		writeError(w, http.StatusInternalServerError, "marker_error", "the bootstrap marker could not be created")
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
