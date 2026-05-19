package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) defineRoutes(mux *http.ServeMux) {

	// Unauthenticated routes
	mux.HandleFunc("/v0/health", s.healthHandler)
	mux.HandleFunc("/v0/bootstrap/lighthouse", s.bootstrapHandler)

	// Authenticated routes
	mux.Handle("/v0/secrets", authenticateClientSecret(http.HandlerFunc(s.handleSecretsCollection)))
	mux.Handle("/v0/secrets/", authenticateClientSecret(http.HandlerFunc(s.handleSecretID)))
	mux.Handle("/v0/auth", authenticateClientSecret(http.HandlerFunc(s.authHandler)))
}

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

func (s *Server) handleSecretsCollection(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v0/secrets" {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getAllSecrets(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed on this route")
	}
}

func (s *Server) handleSecretID(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/v0/secrets/") {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/v0/secrets/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_key", "a secret key is required in the path")
		return
	}

	if err := validateKey(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_key", err.Error())
		return
	}

	source := r.Header.Get("X-Cove-Source")
	if source == "" {
		writeError(w, http.StatusBadRequest, "missing_source", "X-Cove-Source header is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getSecret(w, r, id, source)
	case http.MethodPost:
		s.postSecret(w, r, id, source)
	case http.MethodDelete:
		s.deleteSecret(w, r, id, source)
	case http.MethodPatch:
		s.patchSecret(w, r, id, source)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "allowed methods: GET, POST, PATCH, DELETE")
	}
}

func (s *Server) bootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is allowed")
		return
	}

	if err := CreateBootstrapMarker(); err != nil {
		writeError(w, http.StatusForbidden, "bootstrap_locked", "bootstrap has already been completed")
		return
	}

	clientSecret := os.Getenv("COVE_CLIENT_SECRET")
	if clientSecret == "" {
		_ = DeleteBootstrapMarker()
		writeError(w, http.StatusInternalServerError, "server_error", "client secret is not configured")
		return
	}

	writeResponse(w, http.StatusOK, struct {
		Secret string `json:"secret"`
	}{Secret: clientSecret})

	fmt.Println("Bootstrap complete")
}

// validateKey enforces that a secret key contains only safe characters and is within the length limit.
func validateKey(key string) error {
	const maxLen = 256
	if len(key) > maxLen {
		return fmt.Errorf("key exceeds the maximum length of %d characters", maxLen)
	}
	for _, c := range key {
		if !isValidKeyChar(c) {
			return fmt.Errorf("key contains invalid character %q; only letters, digits, hyphens, underscores, and dots are allowed", c)
		}
	}
	return nil
}

func isValidKeyChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.'
}

func CreateBootstrapMarker() error {
	dir := os.Getenv("APP_MARKER_PATH")
	if dir == "" {
		dir = "/app/vault/markers"
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create marker directory: %w", err)
	}

	marker := filepath.Join(dir, "bootstrap_completed")

	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to create marker file: %w", err)
	}
	_ = f.Close()

	return nil
}

func DeleteBootstrapMarker() error {
	dir := os.Getenv("APP_MARKER_PATH")
	if dir == "" {
		dir = "/app/vault/markers"
	}

	marker := filepath.Join(dir, "bootstrap_completed")

	if err := os.Remove(marker); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove marker file: %w", err)
		}
	}

	return nil
}
