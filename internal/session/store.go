package session

import (
	"context"

	"encoding/json"
)

// Store defines the persistence interface for conversation sessions.
// Implementations must be safe for concurrent use.
type Store interface {
	// Create creates a new session and returns it.
	Create(ctx context.Context, title string) (*Session, error)

	// AppendMessage appends a JSON-encoded message to the session's JSONL.
	AppendMessage(ctx context.Context, sessionID string, msg json.RawMessage) error

	// LoadMessages reads all messages from a session's JSONL.
	LoadMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error)

	// List returns all sessions sorted by UpdatedAt descending.
	List(ctx context.Context) ([]Session, error)

	// Get returns a single session by ID.
	Get(ctx context.Context, id string) (*Session, error)

	// Rename updates the title of a session.
	Rename(ctx context.Context, id, newTitle string) error

	// Delete removes a session's JSONL and meta files.
	Delete(ctx context.Context, id string) error

	// UpdateMeta updates session metadata (model, context window, token counts).
	UpdateMeta(ctx context.Context, sessionID string, meta SessionMeta) error
}
