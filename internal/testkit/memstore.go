package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhanglvtao/cece/internal/session"
)

// MemStore is an in-memory implementation of session.Store, intended for
// tests. It avoids touching the filesystem so tests stay hermetic.
type MemStore struct {
	mu        sync.Mutex
	sessions  map[string]*session.Session
	messages  map[string][]json.RawMessage
	idCounter atomic.Uint64
}

// NewMemStore creates an empty in-memory session store.
func NewMemStore() *MemStore {
	return &MemStore{
		sessions: make(map[string]*session.Session),
		messages: make(map[string][]json.RawMessage),
	}
}

func (s *MemStore) nextID() string {
	n := s.idCounter.Add(1)
	return fmt.Sprintf("test-session-%d", n)
}

// Create creates a new session.
func (s *MemStore) Create(_ context.Context, title string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	sess := &session.Session{
		ID:        s.nextID(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.sessions[sess.ID] = sess
	s.messages[sess.ID] = nil
	cp := *sess
	return &cp, nil
}

// AppendMessage records a single JSONL line for a session.
func (s *MemStore) AppendMessage(_ context.Context, sessionID string, msg json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("memstore: session %s not found", sessionID)
	}

	cp := make(json.RawMessage, len(msg))
	copy(cp, msg)
	s.messages[sessionID] = append(s.messages[sessionID], cp)
	sess.MessageCount++
	sess.UpdatedAt = time.Now()

	var partial struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(msg, &partial) == nil && partial.Role == "user" && partial.Content != "" {
		sess.Preview = truncatePreview(partial.Content)
	}
	return nil
}

// LoadMessages returns a copy of all stored messages for a session.
func (s *MemStore) LoadMessages(_ context.Context, sessionID string) ([]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src, ok := s.messages[sessionID]
	if !ok {
		return nil, nil
	}
	out := make([]json.RawMessage, len(src))
	for i, m := range src {
		cp := make(json.RawMessage, len(m))
		copy(cp, m)
		out[i] = cp
	}
	return out, nil
}

// List returns all sessions sorted by UpdatedAt descending.
func (s *MemStore) List(_ context.Context) ([]session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

// Get returns a session by ID.
func (s *MemStore) Get(_ context.Context, id string) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("memstore: session %s not found", id)
	}
	cp := *sess
	return &cp, nil
}

// Rename updates the title of a session.
func (s *MemStore) Rename(_ context.Context, id, newTitle string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("memstore: session %s not found", id)
	}
	sess.Title = newTitle
	sess.UpdatedAt = time.Now()
	return nil
}

// Delete removes a session and its messages.
func (s *MemStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	delete(s.messages, id)
	return nil
}

// UpdateMeta updates the per-session metadata.
func (s *MemStore) UpdateMeta(_ context.Context, sessionID string, meta session.SessionMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("memstore: session %s not found", sessionID)
	}
	sess.Model = meta.Model
	sess.ContextWindow = meta.ContextWindow
	sess.Protocol = meta.Protocol
	sess.ConfigName = meta.ConfigName
	sess.LastInputTokens = meta.LastInputTokens
	sess.TotalInputTokens = meta.TotalInputTokens
	sess.TotalOutputTokens = meta.TotalOutputTokens
	sess.StatusBar = meta.StatusBar
	sess.UpdatedAt = time.Now()
	return nil
}

// Compile-time check that MemStore implements session.Store.
var _ session.Store = (*MemStore)(nil)

func truncatePreview(s string) string {
	const max = 80
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
