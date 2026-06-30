package tool

import (
	"path/filepath"
	"sync"
)

// ReadTracker records which files have been read during an agent session.
// It is shared (by pointer) across the Read, Edit, and Write tools of a single
// registry so that write-effect tools can enforce a Read-before-Edit guard.
//
// A nil *ReadTracker is safe to use: MarkRead is a no-op and WasRead returns
// true, which makes the guard inert when no tracker is wired (e.g. in tests).
type ReadTracker struct {
	mu   sync.Mutex
	read map[string]struct{}
}

// NewReadTracker creates an empty ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{read: make(map[string]struct{})}
}

// MarkRead records that path has been read.
func (t *ReadTracker) MarkRead(path string) {
	if t == nil {
		return
	}
	key := normalizeTrackerPath(path)
	if key == "" {
		return
	}
	t.mu.Lock()
	if t.read == nil {
		t.read = make(map[string]struct{})
	}
	t.read[key] = struct{}{}
	t.mu.Unlock()
}

// WasRead reports whether path was read earlier. A nil tracker returns true so
// callers without a wired tracker do not trip the guard.
func (t *ReadTracker) WasRead(path string) bool {
	if t == nil {
		return true
	}
	key := normalizeTrackerPath(path)
	if key == "" {
		return false
	}
	t.mu.Lock()
	_, ok := t.read[key]
	t.mu.Unlock()
	return ok
}

// normalizeTrackerPath resolves path to an absolute, cleaned form so that
// relative and absolute references to the same file compare equal. On error it
// falls back to filepath.Clean.
func normalizeTrackerPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
