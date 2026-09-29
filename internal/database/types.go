package database

import "time"

// Secret is a row of cove.secrets. EncryptedValue is never plaintext.
type Secret struct {
	ID             string
	Key            string
	EncryptedValue string
	Version        int
	ReadCount      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type EventKind string

const (
	EventCreate EventKind = "create"
	EventRead   EventKind = "read"
	EventUpdate EventKind = "update"
	EventDelete EventKind = "delete"
	EventRename EventKind = "rename"
)

type EventLogInput struct {
	SecretID          string
	SecretKey         string
	SecretVersion     int
	Kind              EventKind
	Source            string
	OldEncryptedValue *string
	NewEncryptedValue *string
	Detail            string // optional context, e.g. "renamed from X"
}

// Event is a row of cove.event_log, without its encrypted values.
type Event struct {
	SecretKey     string
	SecretVersion int
	Kind          EventKind
	Source        string
	Detail        string
	OccurredAt    time.Time
}
