package database

import "time"

type Secret struct {
	Id           string
	Key          string
	Value        string
	Version      int
	TimesPulled  int
	DateAdded    time.Time
	LastModified time.Time
}

type EventModification string

const (
	EventCreate EventModification = "create"
	EventRead   EventModification = "read"
	EventUpdate EventModification = "update"
	EventDelete EventModification = "delete"
)

type EventLogInput struct {
	SecretID     string
	SecretKey    string
	Version      int
	Modification EventModification
	Source       string
	OldValue     *string
	NewValue     *string
}
