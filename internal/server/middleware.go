package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireClientSecret is middleware that validates the Bearer token in the Authorization header.
func (s *Server) requireClientSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing_token", "Authorization header is required")
			return
		}

		tokenParts := strings.SplitN(authHeader, " ", 2)
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			writeError(w, http.StatusUnauthorized, "invalid_token_format", "Authorization header must be in the form: Bearer <token>")
			return
		}

		provided := []byte(tokenParts[1])
		stored := []byte(s.clientSecret)

		if subtle.ConstantTimeCompare(provided, stored) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid_token", "the provided token is invalid")
			return
		}

		next.ServeHTTP(w, r)
	})
}
