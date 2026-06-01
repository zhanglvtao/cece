package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"cece/internal/protocol"
	"cece/internal/remote"
)

// EngineProc represents an engine subprocess managed by the hub.
type EngineProc struct {
	SessionID string
	PID       int
	SocketPath string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
}

// StartEngine launches an engine subprocess in --socket mode for the given session.
func (h *Hub) StartEngine(sessionID string) (*EngineProc, error) {
	socketPath := EngineSocketPath(h.projectDir, sessionID)

	bin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find executable: %w", err)
	}

	ctx, cancel := context.WithCancel(h.ctx)

	cmd := exec.CommandContext(ctx, bin,
		"engine",
		"--socket", socketPath,
		"--session-id", sessionID,
		"--project-dir", h.projectDir,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start engine: %w", err)
	}

	proc := &EngineProc{
		SessionID:  sessionID,
		PID:        cmd.Process.Pid,
		SocketPath: socketPath,
		cmd:        cmd,
		cancel:     cancel,
	}

	// Wait for the socket file to appear (engine is ready).
	if err := h.waitForSocket(socketPath, 5*time.Second); err != nil {
		proc.Kill()
		return nil, fmt.Errorf("engine socket not ready: %w", err)
	}

	// Monitor the process in the background.
	go h.monitorEngine(proc)

	return proc, nil
}

// Kill terminates the engine subprocess.
func (p *EngineProc) Kill() {
	p.cancel()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	os.Remove(p.SocketPath)
}

// waitForSocket polls until the socket file exists.
func (h *Hub) waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("timeout waiting for socket %s", path)
}

// monitorEngine waits for the engine process to exit and cleans up.
func (h *Hub) monitorEngine(proc *EngineProc) {
	err := proc.cmd.Wait()
	if err != nil {
		slog.Info("engine process exited", "session", proc.SessionID, "pid", proc.PID, "error", err)
	}

	os.Remove(proc.SocketPath)

	h.mu.Lock()
	if s, ok := h.sessions[proc.SessionID]; ok && s.EnginePID == proc.PID {
		s.Status = SessionDetached
		s.EnginePID = 0
		s.SocketPath = ""
	}
	h.mu.Unlock()
}

// SendInput connects to an engine's socket and sends an InputAction.
func (h *Hub) SendInput(sessionID, text string) error {
	socketPath := EngineSocketPath(h.projectDir, sessionID)
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	client, err := remote.NewSocket(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("connect to engine: %w", err)
	}
	defer client.Close()

	if err := client.Input(ctx, text); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	return nil
}

// CancelEngine connects to an engine's socket and sends a CancelAction.
func (h *Hub) CancelEngine(sessionID string) error {
	socketPath := EngineSocketPath(h.projectDir, sessionID)
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()

	client, err := remote.NewSocket(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("connect to engine: %w", err)
	}
	defer client.Close()

	client.Do(protocol.CancelAction{})
	return nil
}

// TailEngine connects to an engine's socket and returns the client
// for streaming events. The caller must close the client.
func (h *Hub) TailEngine(ctx context.Context, sessionID string) (*remote.Client, error) {
	socketPath := EngineSocketPath(h.projectDir, sessionID)
	return remote.NewSocket(ctx, socketPath)
}

// CreateAndStartSession creates a new session and starts an engine for it.
func (h *Hub) CreateAndStartSession(source SessionSource) (*ManagedSession, error) {
	sess, err := h.store.Create(h.ctx, "New session")
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	proc, err := h.StartEngine(sess.ID)
	if err != nil {
		h.store.Delete(h.ctx, sess.ID)
		return nil, fmt.Errorf("start engine: %w", err)
	}

	ms := &ManagedSession{
		ID:         sess.ID,
		Title:      sess.Title,
		Status:     SessionIdle,
		Source:     source,
		EnginePID:  proc.PID,
		SocketPath: proc.SocketPath,
		CreatedAt:  sess.CreatedAt,
		UpdatedAt:  sess.UpdatedAt,
	}

	h.mu.Lock()
	h.sessions[sess.ID] = ms
	h.mu.Unlock()

	return ms, nil
}

// DialEngineSocket is a convenience to get a remote.Client connected to
// an engine's socket. Used by "cece hub tui".
func DialEngineSocket(ctx context.Context, projectDir, sessionID string) (*remote.Client, error) {
	socketPath := EngineSocketPath(projectDir, sessionID)
	return remote.NewSocket(ctx, socketPath)
}

// EngineSocketPath returns the Unix socket path for a session's engine.
func EngineSocketPath(projectDir, sessionID string) string {
	return filepath.Join(projectDir, ".cece", SessionsDir, sessionID+".sock")
}
