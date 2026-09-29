package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/LSariol/Cove/internal/vault"
)

const maxBodyBytes = 1 << 16 // 64 KB

const invalidBodyMessage = `request body must be JSON like {"value": "..."} and at most 64 KB`

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

	if err := vault.ValidateKey(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_key", err.Error())
		return
	}

	source := r.Header.Get("X-Cove-Source")
	if source == "" {
		writeError(w, http.StatusBadRequest, "missing_source", "X-Cove-Source header is required (the name of the calling app)")
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

func (s *Server) getAllSecrets(w http.ResponseWriter, r *http.Request) {
	secrets, err := s.vault.List(r.Context())
	if err != nil {
		log.Printf("list secrets: %v", err)
		writeError(w, http.StatusInternalServerError, "get_all", "failed to retrieve secrets")
		return
	}

	// Always an array, even when empty: clients shouldn't have to handle null.
	pubList := SecretSummaryList{Secrets: []SecretSummary{}}
	for _, secret := range secrets {
		pubList.Secrets = append(pubList.Secrets, SecretSummary{
			Key:         secret.Key,
			Version:     secret.Version,
			TimesPulled: secret.ReadCount,
			CreatedAt:   secret.CreatedAt,
			UpdatedAt:   secret.UpdatedAt,
		})
	}

	writeResponse(w, http.StatusOK, pubList)
}

func (s *Server) getSecret(w http.ResponseWriter, r *http.Request, id string, source string) {
	secret, err := s.vault.Get(r.Context(), id, source)
	switch {
	case errors.Is(err, vault.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
		return
	case errors.Is(err, vault.ErrDecrypt):
		log.Printf("get %q (source %q) failed: %v", id, source, err)
		writeError(w, http.StatusInternalServerError, "decrypt_error", "the secret exists but couldn't be decrypted; VAULT_ENCRYPTION_KEY may have changed")
		return
	case err != nil:
		log.Printf("get %q (source %q) failed: %v", id, source, err)
		writeError(w, http.StatusInternalServerError, "read_error", "the secret could not be read")
		return
	}

	writeResponse(w, http.StatusOK, GetSecretResponse{
		Key:     secret.Key,
		Value:   secret.Value,
		Version: secret.Version,
	})
}

func (s *Server) postSecret(w http.ResponseWriter, r *http.Request, key string, source string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", invalidBodyMessage)
		return
	}

	if err := s.vault.Create(r.Context(), key, body.Value, source); err != nil {
		if errors.Is(err, vault.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "already_exists", "a secret with this key already exists; use PATCH to change its value")
			return
		}
		log.Printf("create %q (source %q) failed: %v", key, source, err)
		writeError(w, http.StatusInternalServerError, "create_error", "failed to create secret")
		return
	}

	writeResponse(w, http.StatusCreated, SecretActionResponse{
		Key:     key,
		Action:  "created",
		Message: fmt.Sprintf("%s has been created.", key),
	})

	log.Printf("created %q (source %q)", key, source)
}

func (s *Server) patchSecret(w http.ResponseWriter, r *http.Request, key string, source string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", invalidBodyMessage)
		return
	}

	if _, err := s.vault.Update(r.Context(), key, body.Value, source); err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no secret with this key exists; use POST to create it")
			return
		}
		log.Printf("update %q (source %q) failed: %v", key, source, err)
		writeError(w, http.StatusInternalServerError, "update_error", "failed to update secret")
		return
	}

	writeResponse(w, http.StatusOK, SecretActionResponse{
		Key:     key,
		Action:  "updated",
		Message: fmt.Sprintf("%s has been updated.", key),
	})

	log.Printf("updated %q (source %q)", key, source)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request, key string, source string) {
	if err := s.vault.Delete(r.Context(), key, source); err != nil {
		if errors.Is(err, vault.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "secret not found")
			return
		}
		log.Printf("delete %q (source %q) failed: %v", key, source, err)
		writeError(w, http.StatusInternalServerError, "delete_error", "the secret could not be deleted")
		return
	}

	writeResponse(w, http.StatusOK, SecretActionResponse{
		Key:     key,
		Action:  "deleted",
		Message: fmt.Sprintf("%s has been deleted.", key),
	})

	log.Printf("deleted %q (source %q)", key, source)
}
