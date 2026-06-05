package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/session"
)

type SessionCoordinator struct {
	store session.Store
}

type SessionStartResult struct {
	ID    string
	Title string
}

func NewSessionCoordinator(store session.Store) *SessionCoordinator {
	return &SessionCoordinator{store: store}
}

func (c *SessionCoordinator) StartTurn(ctx context.Context, input string, currentSessionID string, sessionCreated bool) SessionStartResult {
	if c.store == nil {
		return SessionStartResult{}
	}
	if !sessionCreated {
		sess, err := c.store.Create(ctx, "")
		if err != nil {
			slog.Error("failed to create session", "error", err)
			return SessionStartResult{}
		}
		title := session.GenerateTitle(input)
		if err := c.store.Rename(ctx, sess.ID, title); err != nil {
			slog.Error("failed to set session title", "error", err)
		}
		logger.SetSessionID(sess.ID)
		return SessionStartResult{ID: sess.ID, Title: title}
	}

	if currentSessionID != "" {
		// Update title with latest user prompt suffix.
		if existing, err := c.store.Get(ctx, currentSessionID); err == nil && existing != nil {
			updated := session.UpdateTitle(existing.Title, input)
			if err := c.store.Rename(ctx, currentSessionID, updated); err != nil {
				slog.Error("failed to update session title", "error", err)
			}
		}
	}
	return SessionStartResult{}
}

// PersistMessage appends a message to the current session's JSONL.
// No-op when persistence is not configured.
func (c *SessionCoordinator) PersistMessage(ctx context.Context, sessionID string, msg Message) {
	if c.store == nil || sessionID == "" {
		return
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal message for persistence", "error", err)
		return
	}
	if err := c.store.AppendMessage(ctx, sessionID, raw); err != nil {
		slog.Error("failed to persist message", "error", err)
	}
}

func (c *SessionCoordinator) UpdateMeta(ctx context.Context, sessionID string, meta session.SessionMeta) {
	if c.store == nil || sessionID == "" {
		return
	}
	if err := c.store.UpdateMeta(ctx, sessionID, meta); err != nil {
		slog.Error("failed to update session meta", "error", err)
	}
}
