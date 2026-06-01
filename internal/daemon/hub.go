package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cece/internal/config"
	"cece/internal/session"
)

const (
	// DefaultSocketPath is the default Unix socket path for the hub.
	DefaultSocketPath = "~/.cece/hub.sock"

	// SessionsDir is the subdirectory within .cece that holds session data.
	SessionsDir = "sessions"
)

// ManagedSession represents a session tracked by the hub.
type ManagedSession struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Status     SessionStatus  `json:"status"`
	Source     SessionSource  `json:"source"`
	EnginePID  int            `json:"engine_pid,omitempty"`
	SocketPath string         `json:"socket_path,omitempty"`
	Model      string         `json:"model,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type SessionStatus string

const (
	SessionIdle       SessionStatus = "idle"
	SessionBusy       SessionStatus = "busy"
	SessionBackground SessionStatus = "background"
	SessionDetached   SessionStatus = "detached"
)

type SessionSource string

const (
	SourceTUI SessionSource = "tui"
	SourceHub SessionSource = "hub"
	SourceCLI SessionSource = "cli"
)

// Hub is the central daemon that manages engine processes and tracks sessions.
type Hub struct {
	socketPath string
	projectDir string
	sessionsDir string
	config     *config.Config
	store      session.Store

	mu       sync.RWMutex
	sessions map[string]*ManagedSession

	ctx    context.Context
	cancel context.CancelFunc
}

// NewHub creates a new Hub instance.
func NewHub(projectDir string) (*Hub, error) {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return nil, err
	}

	ceceDir := filepath.Join(projectDir, ".cece")
	if err := os.MkdirAll(ceceDir, 0o755); err != nil {
		return nil, err
	}

	socketPath := filepath.Join(ceceDir, "hub.sock")
	sessionsDir := filepath.Join(ceceDir, SessionsDir)
	store := session.NewFileStore(projectDir)

	ctx, cancel := context.WithCancel(context.Background())

	return &Hub{
		socketPath:  socketPath,
		projectDir:  projectDir,
		sessionsDir: sessionsDir,
		config:      &cfg,
		store:      store,
		sessions:   make(map[string]*ManagedSession),
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Run starts the hub: loads existing sessions, starts watchers and the RPC server.
// It blocks until ctx is cancelled or a fatal error occurs.
func (h *Hub) Run() error {
	slog.Info("cece hub starting", "project", h.projectDir, "socket", h.socketPath)

	// Load existing sessions from disk.
	h.loadExistingSessions()

	// Start the session watcher (fsnotify).
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		h.watchSessions()
	}()

	// Start the RPC server.
	if err := h.serve(); err != nil {
		h.cancel()
		<-watcherDone
		return err
	}

	<-watcherDone
	return nil
}

// Shutdown signals the hub to stop.
func (h *Hub) Shutdown() {
	h.cancel()
}

// SocketPath returns the hub's Unix socket path.
func (h *Hub) SocketPath() string { return h.socketPath }

// loadExistingSessions scans the session store and registers all found sessions
// as detached (we don't know if they have running engines).
func (h *Hub) loadExistingSessions() {
	sessions, err := h.store.List(h.ctx)
	if err != nil {
		slog.Warn("hub failed to load sessions", "error", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range sessions {
		h.sessions[s.ID] = &ManagedSession{
			ID:        s.ID,
			Title:     s.Title,
			Status:    SessionDetached,
			Source:    SourceTUI, // assume TUI-created since we don't know
			Model:     s.Model,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		}
	}
	slog.Info("hub loaded sessions", "count", len(h.sessions))
}

// GetSession returns a managed session by ID.
func (h *Hub) GetSession(id string) *ManagedSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[id]
}

// ListSessions returns all managed sessions.
func (h *Hub) ListSessions() []*ManagedSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*ManagedSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, s)
	}
	return out
}

// setStatus updates the status of a managed session.
func (h *Hub) setStatus(id string, status SessionStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[id]; ok {
		s.Status = status
		s.UpdatedAt = time.Now()
	}
}
