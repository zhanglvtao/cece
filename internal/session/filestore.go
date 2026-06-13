package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// FileStore implements Store using JSONL + meta.json files on disk.
type FileStore struct {
	dir string // .cece/sessions/
	mu  sync.Mutex
}

// NewFileStore creates a FileStore rooted at {projectDir}/.cece/sessions/.
// Dir returns the directory where session files are stored.
func (s *FileStore) Dir() string { return s.dir }

func NewFileStore(projectDir string) *FileStore {
	dir := filepath.Join(projectDir, ".cece", "sessions")
	return &FileStore{dir: dir}
}

func (s *FileStore) jsonlPath(id string) string { return filepath.Join(s.dir, id+".jsonl") }
func (s *FileStore) metaPath(id string) string  { return filepath.Join(s.dir, id+".meta.json") }

// Create creates a new session with the given title.
func (s *FileStore) Create(_ context.Context, title string) (*Session, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	now := time.Now()
	sess := &Session{
		ID:        uuid.New().String(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create empty JSONL
	if f, err := os.OpenFile(s.jsonlPath(sess.ID), os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
		return nil, fmt.Errorf("create jsonl: %w", err)
	} else {
		f.Close()
	}

	if err := s.writeMeta(sess); err != nil {
		os.Remove(s.jsonlPath(sess.ID))
		return nil, fmt.Errorf("write meta: %w", err)
	}

	return sess, nil
}

// AppendMessage appends a JSON-encoded message to the session's JSONL.
// It also updates the session's MessageCount and Preview (last user message) in meta.
func (s *FileStore) AppendMessage(_ context.Context, sessionID string, msg json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.jsonlPath(sessionID), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(msg, '\n')); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync jsonl: %w", err)
	}

	// Update MessageCount and Preview in meta.
	sess, err := s.readMeta(sessionID)
	if err != nil {
		return nil // best-effort
	}
	sess.MessageCount++
	sess.UpdatedAt = time.Now()

	// Extract preview from user messages.
	var partial struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(msg, &partial) == nil && partial.Role == "user" && partial.Content != "" {
		sess.Preview = truncatePreview(partial.Content)
	}

	// Best-effort write back.
	_ = s.writeMeta(sess)

	return nil
}

// LoadMessages reads all messages from a session's JSONL.
func (s *FileStore) LoadMessages(_ context.Context, sessionID string) ([]json.RawMessage, error) {
	f, err := os.Open(s.jsonlPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	defer f.Close()

	var messages []json.RawMessage
	scanner := bufio.NewScanner(f)
	// Allow large lines (tool results can be big)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			slog.Warn("skipping corrupt jsonl line", "session", sessionID, "line", lineNum)
			continue
		}
		// Copy the line since scanner reuses its buffer
		cp := make(json.RawMessage, len(line))
		copy(cp, line)
		messages = append(messages, cp)
	}
	if err := scanner.Err(); err != nil {
		return messages, fmt.Errorf("scan jsonl: %w", err)
	}
	return messages, nil
}

// List returns all sessions sorted by UpdatedAt descending.
func (s *FileStore) List(_ context.Context) ([]Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".meta.json")
		sess, err := s.readMeta(id)
		if err != nil {
			slog.Warn("skipping corrupt meta file", "file", entry.Name(), "error", err)
			continue
		}
		sessions = append(sessions, *sess)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// Get returns a single session by ID.
func (s *FileStore) Get(_ context.Context, id string) (*Session, error) {
	sess, err := s.readMeta(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session %s: not found", id)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// Rename updates the title of a session.
func (s *FileStore) Rename(_ context.Context, id, newTitle string) error {
	sess, err := s.readMeta(id)
	if err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	sess.Title = newTitle
	sess.UpdatedAt = time.Now()
	return s.writeMeta(sess)
}

// Delete removes a session's JSONL and meta files.
func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err1 := os.Remove(s.jsonlPath(id))
	err2 := os.Remove(s.metaPath(id))
	if err1 != nil && !os.IsNotExist(err1) {
		return fmt.Errorf("delete session %s: %w", id, err1)
	}
	if err2 != nil && !os.IsNotExist(err2) {
		return fmt.Errorf("delete session %s meta: %w", id, err2)
	}
	return nil
}

// writeMeta writes session metadata atomically (write-to-temp + rename).
func (s *FileStore) writeMeta(sess *Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}

	dst := s.metaPath(sess.ID)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp meta: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

// readMeta reads session metadata from the meta.json file.
func (s *FileStore) readMeta(id string) (*Session, error) {
	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal meta: %w", err)
	}
	return &sess, nil
}

// touchMeta updates the UpdatedAt timestamp without changing other fields.
func (s *FileStore) touchMeta(id string) {
	sess, err := s.readMeta(id)
	if err != nil {
		return
	}
	sess.UpdatedAt = time.Now()
	// Best-effort; ignore errors
	s.writeMeta(sess)
}

// UpdateMeta updates session metadata (model, context window, token counts).
func (s *FileStore) UpdateMeta(_ context.Context, sessionID string, meta SessionMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.readMeta(sessionID)
	if err != nil {
		return fmt.Errorf("update meta: %w", err)
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
	return s.writeMeta(sess)
}

// SaveInputHistory persists the input history for a session by writing
// it to the session's meta.json file. History is capped at 100 entries.
func (s *FileStore) SaveInputHistory(_ context.Context, sessionID string, history []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.readMeta(sessionID)
	if err != nil {
		return fmt.Errorf("save input history: %w", err)
	}
	if len(history) > 100 {
		history = history[:100]
	}
	sess.InputHistory = history
	sess.UpdatedAt = time.Now()
	return s.writeMeta(sess)
}

// UpdateRelation persists parent-child agent relationship on a session.
// Implements RelationStore.
func (s *FileStore) UpdateRelation(_ context.Context, sessionID string, parentID string, agentID string, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.readMeta(sessionID)
	if err != nil {
		return fmt.Errorf("update relation: %w", err)
	}
	sess.ParentID = parentID
	sess.AgentID = agentID
	sess.Kind = kind
	sess.UpdatedAt = time.Now()
	return s.writeMeta(sess)
}
