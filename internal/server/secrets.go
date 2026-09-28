package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

const maxBodyBytes = 1 << 16 // 64 KB

func (s *Server) getAllSecrets(w http.ResponseWriter, r *http.Request) {
	keys, err := s.DB.GetAllKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get_all", "failed to retrieve secrets")
		return
	}

	var pubList SecretSummaryList
	for _, key := range keys {
		pubList.Secrets = append(pubList.Secrets, SecretSummary{
			Key:         key.Key,
			Version:     key.Version,
			TimesPulled: key.ReadCount,
			CreatedAt:   key.CreatedAt,
			UpdatedAt:   key.UpdatedAt,
		})
	}

	writeResponse(w, http.StatusOK, pubList)
}

func (s *Server) getSecret(w http.ResponseWriter, r *http.Request, id string, source string) {
	secret, err := s.DB.GetSecret(r.Context(), id, source)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
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
		writeError(w, http.StatusBadRequest, "invalid_body", "request body is invalid or too large")
		return
	}

	if err := s.DB.CreateSecret(r.Context(), key, body.Value, source); err != nil {
		writeError(w, http.StatusInternalServerError, "create_error", "failed to create secret")
		return
	}

	writeResponse(w, http.StatusCreated, SecretActionResponse{
		Key:     key,
		Action:  "created",
		Message: fmt.Sprintf("%s has been created.", key),
	})

	log.Printf("%s has been created.\n", key)
}

func (s *Server) patchSecret(w http.ResponseWriter, r *http.Request, key string, source string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body is invalid or too large")
		return
	}

	if err := s.DB.UpdateSecret(r.Context(), key, body.Value, source); err != nil {
		writeError(w, http.StatusInternalServerError, "update_error", "failed to update secret")
		return
	}

	writeResponse(w, http.StatusOK, SecretActionResponse{
		Key:     key,
		Action:  "updated",
		Message: fmt.Sprintf("%s has been updated.", key),
	})

	log.Printf("%s has been updated.\n", key)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request, key string, source string) {
	if err := s.DB.DeleteSecret(r.Context(), key, source); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "secret not found")
		return
	}

	writeResponse(w, http.StatusOK, SecretActionResponse{
		Key:     key,
		Action:  "deleted",
		Message: fmt.Sprintf("%s has been deleted.", key),
	})

	log.Printf("%s has been deleted.\n", key)
}

// writeResponse sends a successful JSON envelope with the given status and data payload.
func writeResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data})
}

// writeError sends a failure JSON envelope with a machine-readable type and human-readable message.
func writeError(w http.ResponseWriter, status int, errType string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error:   &APIError{Type: errType, Message: message},
	})
}
