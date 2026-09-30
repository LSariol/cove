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

// maxBatchKeys is the most keys one batch request may ask for.
const maxBatchKeys = 100

// BatchRequest is the body of POST /v0/batch.
type BatchRequest struct {
	Keys []string `json:"keys"`
}

// BatchResponse is the data of a successful POST /v0/batch: the secrets in
// the order asked for, duplicates once.
type BatchResponse struct {
	Secrets []GetSecretResponse `json:"secrets"`
}

// batchHandler reads several secrets in one request (e.g. everything a
// project's compose file asks for). It's all or nothing, and the checks run
// in this order, so the answer never reveals more than a single read would:
//
//  1. The body lists 1–100 valid keys.
//  2. A project token must be able to read every key. If not, 403 without
//     saying which: the server log names them, for the operator.
//  3. Every key must exist. If not, 404 naming every missing key (the caller
//     is allowed to read them, so their names aren't news to it).
//  4. Every value is read and counted in one transaction.
func (s *Server) batchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is allowed on this route")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var body BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", `request body must be JSON like {"keys": ["a", "b"]} and at most 64 KB`)
		return
	}
	if len(body.Keys) == 0 || len(body.Keys) > maxBatchKeys {
		writeError(w, http.StatusBadRequest, "invalid_body", fmt.Sprintf("keys must list 1 to %d secret keys", maxBatchKeys))
		return
	}
	for _, key := range body.Keys {
		if err := vault.ValidateKey(key); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_key", fmt.Sprintf("%q: %v", key, err))
			return
		}
	}

	c := callerFrom(r.Context())
	source := r.Header.Get("X-Cove-Source")
	if c.project != nil {
		source = c.project.Name
		var refused []string
		for _, key := range body.Keys {
			if !c.project.CanRead(key) {
				refused = append(refused, key)
			}
		}
		if len(refused) > 0 {
			log.Printf("batch: %s's token can't read %s; refused the whole request", c.project.Name, strings.Join(refused, ", "))
			writeError(w, http.StatusForbidden, "forbidden_key", fmt.Sprintf("%s's token can't read one or more of the requested keys", c.project.Name))
			return
		}
	} else if source == "" {
		writeError(w, http.StatusBadRequest, "missing_source", "X-Cove-Source header is required (the name of the calling app)")
		return
	}

	secrets, err := s.vault.GetMany(r.Context(), body.Keys, source)
	var missing *vault.MissingError
	switch {
	case errors.As(err, &missing):
		writeErrorWithKeys(w, http.StatusNotFound, "not_found", "no secret named "+strings.Join(missing.Keys, ", "), missing.Keys)
		return
	case errors.Is(err, vault.ErrDecrypt):
		log.Printf("batch (source %q) failed: %v", source, err)
		writeError(w, http.StatusInternalServerError, "decrypt_error", "a secret exists but couldn't be decrypted; VAULT_ENCRYPTION_KEY may have changed")
		return
	case err != nil:
		log.Printf("batch (source %q) failed: %v", source, err)
		writeError(w, http.StatusInternalServerError, "read_error", "the secrets could not be read")
		return
	}

	resp := BatchResponse{Secrets: make([]GetSecretResponse, 0, len(secrets))}
	for _, secret := range secrets {
		resp.Secrets = append(resp.Secrets, GetSecretResponse{Key: secret.Key, Value: secret.Value, Version: secret.Version})
	}
	writeResponse(w, http.StatusOK, resp)
}
