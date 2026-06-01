package daemon

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// watchSessions monitors the .cece/sessions/ directory for changes
// and updates the hub's session registry accordingly.
func (h *Hub) watchSessions() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("hub watcher init failed", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(h.sessionsDir); err != nil {
		slog.Error("hub watcher add dir failed", "dir", h.sessionsDir, "error", err)
		return
	}

	for {
		select {
		case <-h.ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			h.handleFSEvent(event)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("hub watcher error", "error", err)
		}
	}
}

func (h *Hub) handleFSEvent(event fsnotify.Event) {
	if !isMetaFile(event.Name) {
		return
	}

	switch {
	case event.Has(fsnotify.Create), event.Has(fsnotify.Write), event.Has(fsnotify.Rename):
		h.syncSessionFromDisk(event.Name)
	case event.Has(fsnotify.Remove):
		h.removeSessionByPath(event.Name)
	}
}

func (h *Hub) syncSessionFromDisk(path string) {
	id := sessionIDFromPath(path)
	if id == "" {
		return
	}

	sess, err := h.store.Get(h.ctx, id)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	existing, ok := h.sessions[id]
	if ok && existing.Source != SourceTUI {
		return
	}

	h.sessions[id] = &ManagedSession{
		ID:        sess.ID,
		Title:     sess.Title,
		Status:    SessionDetached,
		Source:    SourceTUI,
		Model:     sess.Model,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
	}
}

func (h *Hub) removeSessionByPath(path string) {
	id := sessionIDFromPath(path)
	if id == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[id]; ok && s.Source == SourceTUI {
		delete(h.sessions, id)
	}
}

// isMetaFile returns true if the path ends with .meta.json.
func isMetaFile(path string) bool {
	return strings.HasSuffix(filepath.Base(path), ".meta.json")
}

// sessionIDFromPath extracts a session ID from a meta.json file path.
// e.g. /path/.cece/sessions/abc123.meta.json → abc123
func sessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".meta.json")
}
