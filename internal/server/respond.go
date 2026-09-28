package server

import (
	"encoding/json"
	"net/http"
)

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
