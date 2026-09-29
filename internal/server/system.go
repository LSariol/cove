package server

import (
	"log"
	"net/http"
	"time"
)

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, struct {
		Healthy bool   `json:"healthy"`
		Time    string `json:"time"`
	}{
		Healthy: true,
		Time:    time.Now().Format(time.RFC3339),
	})
}

// readyHandler reports whether Cove can serve secrets, i.e. the database is
// reachable. /v0/health only says the HTTP server is up; use this for Docker
// healthchecks and for waiting on Cove at startup.
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		log.Printf("readiness check: database unreachable: %v", err)
		writeError(w, http.StatusServiceUnavailable, "not_ready", "the database is unreachable")
		return
	}

	writeResponse(w, http.StatusOK, struct {
		Ready bool   `json:"ready"`
		Time  string `json:"time"`
	}{
		Ready: true,
		Time:  time.Now().Format(time.RFC3339),
	})
}

func (s *Server) authHandler(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, struct {
		Authenticated bool   `json:"authenticated"`
		Time          string `json:"time"`
	}{
		Authenticated: true,
		Time:          time.Now().Format(time.RFC3339),
	})
}
