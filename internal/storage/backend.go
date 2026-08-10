package storage

// Package storage provides the mail storage abstraction layer.
// It defines the Backend interface that protocol engines use to
// persist and retrieve messages, decoupling them from the
// underlying filesystem implementation.

import (
	"context"
	"time"
)

// Message represents a complete email message for writing.
// Used by SMTP engine when committing a received message.
type Message struct {
	From string
	To []string
	Subject string
	Body string
}

// MessageMetadata contains lightweight message info for listing.
// Intentionally excludes body to avoid loading full messages
// during STAT/LIST operations.
type MessageMetadata struct {
	ID        string    // Filename-based unique identifier
	SizeBytes int64     // File size in bytes
	Timestamp time.Time // From filename or file mtime
}

// backend defines the contract fo mail storage ops
// all methods should be safe for concurrent use across go routinees
type Backend interface {
	// WriteMessage atomically persists a message to a user's inbox.
	// Returns the generated message ID on success.
	// Context allows cancellation if client disconnects mid-write.
	WriteMessage(ctx context.Context, user string, msg *Message) (string, error)

	// ListMessages returns metadata for all non-deleted messages.
	// Results are sorted chronologically by filename.
	ListMessages(user string) ([]MessageMetadata, error)

	// GetMessage retrieves raw .eml bytes by message ID.
	// ID is sanitized internally to prevent directory traversal.
	GetMessage(user string, id string) ([]byte, error)

	// DeleteMessage removes a message file from disk.
	// Called during POP3 UPDATE phase after QUIT.
	DeleteMessage(user string, id string) error

	// UserExists checks if a user's mailbox directory exists.
	UserExists(user string) bool
}