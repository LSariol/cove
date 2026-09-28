package database

import "time"

type Secret struct {
	Id        string
	Key       string
	Value     string
	Version   int
	ReadCount int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EventKind string

const (
	EventCreate EventKind = "create"
	EventRead   EventKind = "read"
	EventUpdate EventKind = "update"
	EventDelete EventKind = "delete"
)

type EventLogInput struct {
	SecretID          string
	SecretKey         string
	SecretVersion     int
	Kind              EventKind
	Source            string
	OldEncryptedValue *string
	NewEncryptedValue *string
}
