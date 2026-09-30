package server

import "net/http"

func (s *Server) defineRoutes(mux *http.ServeMux) {

	// Unauthenticated routes
	mux.Handle("/v0/health", getOnly(http.HandlerFunc(s.healthHandler)))
	mux.Handle("/v0/ready", getOnly(http.HandlerFunc(s.readyHandler)))
	mux.HandleFunc("/v0/bootstrap/lighthouse", s.bootstrapHandler)

	// Authenticated routes
	mux.Handle("/v0/secrets", s.requireToken(http.HandlerFunc(s.handleSecretsCollection)))
	mux.Handle("/v0/secrets/", s.requireToken(http.HandlerFunc(s.handleSecretID)))
	mux.Handle("/v0/batch", s.requireToken(http.HandlerFunc(s.batchHandler)))
	mux.Handle("/v0/auth", s.requireToken(getOnly(http.HandlerFunc(s.authHandler))))
	mux.Handle("/v0/version", s.requireToken(getOnly(http.HandlerFunc(s.versionHandler))))
}

// getOnly answers anything but GET (or HEAD) with 405.
func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed on this route")
			return
		}
		next.ServeHTTP(w, r)
	})
}
