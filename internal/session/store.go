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

	// SaveInputHistory persists the input history for a session.
	SaveInputHistory(ctx context.Context, sessionID string, history []string) error
}

// RelationStore is an optional interface that Store implementations can
// satisfy to persist parent-child agent relationships.
type RelationStore interface {
	UpdateRelation(ctx context.Context, sessionID string, parentID string, agentID string, kind string) error
}

// ArtifactStore is an optional interface that Store implementations can
// satisfy to write and read agent artifacts (e.g. subagent result files).
// The returned path must be absolute and suitable for direct use with the
// Read tool. Path strategy is entirely owned by the store implementation.
type ArtifactStore interface {
	WriteArtifact(ctx context.Context, sessionID, name string, content []byte) (absolutePath string, err error)
	ArtifactPath(ctx context.Context, sessionID, name string) (absolutePath string, err error)
}
