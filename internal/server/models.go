package server

import "time"

// APIResponse is the uniform envelope for every response this server sends.
type APIResponse struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError carries machine-readable and human-readable error detail.
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// SecretSummary is the public (non-sensitive) metadata for a single secret.
type SecretSummary struct {
	Key         string    `json:"key"`
	Version     int       `json:"version"`
	TimesPulled int       `json:"times_pulled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SecretSummaryList struct {
	Secrets []SecretSummary `json:"secrets"`
}

type GetSecretResponse struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version int    `json:"version"`
}

// SecretActionResponse is returned by create, update, and delete operations.
type SecretActionResponse struct {
	Key     string `json:"key"`
	Action  string `json:"action"`  // "created" | "updated" | "deleted"
	Message string `json:"message"` // human-readable confirmation
}
