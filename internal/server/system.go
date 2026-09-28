package server

import (
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

func (s *Server) authHandler(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, http.StatusOK, struct {
		Authenticated bool   `json:"authenticated"`
		Time          string `json:"time"`
	}{
		Authenticated: true,
		Time:          time.Now().Format(time.RFC3339),
	})
}
