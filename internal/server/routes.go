package server

import "net/http"

func (s *Server) defineRoutes(mux *http.ServeMux) {

	// Unauthenticated routes
	mux.HandleFunc("/v0/health", s.healthHandler)
	mux.HandleFunc("/v0/ready", s.readyHandler)
	mux.HandleFunc("/v0/bootstrap/lighthouse", s.bootstrapHandler)

	// Authenticated routes
	mux.Handle("/v0/secrets", s.requireClientSecret(http.HandlerFunc(s.handleSecretsCollection)))
	mux.Handle("/v0/secrets/", s.requireClientSecret(http.HandlerFunc(s.handleSecretID)))
	mux.Handle("/v0/auth", s.requireClientSecret(http.HandlerFunc(s.authHandler)))
	mux.Handle("/v0/version", s.requireClientSecret(http.HandlerFunc(s.versionHandler)))
}
