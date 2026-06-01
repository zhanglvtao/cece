package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
)

// HubRequest is a JSON-RPC style request to the hub.
type HubRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// HubResponse is a JSON-RPC style response from the hub.
type HubResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// serve starts the Unix socket RPC server and blocks until the hub is shut down.
func (h *Hub) serve() error {
	os.Remove(h.socketPath)

	l, err := net.Listen("unix", h.socketPath)
	if err != nil {
		return fmt.Errorf("hub listen %s: %w", h.socketPath, err)
	}

	go func() {
		<-h.ctx.Done()
		l.Close()
		os.Remove(h.socketPath)
	}()

	slog.Info("hub rpc listening", "socket", h.socketPath)

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-h.ctx.Done():
				return nil
			default:
				return fmt.Errorf("hub accept: %w", err)
			}
		}
		go h.handleConn(conn)
	}
}

func (h *Hub) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	for scanner.Scan() {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		var req HubRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			h.writeResponse(conn, HubResponse{Error: err.Error()})
			continue
		}

		resp := h.dispatch(req.Method, req.Params)
		h.writeResponse(conn, resp)
	}
}

func (h *Hub) writeResponse(conn net.Conn, resp HubResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("hub marshal response", "error", err)
		return
	}
	conn.Write(append(data, '\n'))
}

func (h *Hub) dispatch(method string, params json.RawMessage) HubResponse {
	switch method {
	case "session.list":
		return h.handleSessionList()
	case "session.create":
		return h.handleSessionCreate()
	case "session.delete":
		return h.handleSessionDelete(params)
	case "session.input":
		return h.handleSessionInput(params)
	case "session.cancel":
		return h.handleSessionCancel(params)
	case "session.get":
		return h.handleSessionGet(params)
	case "hub.status":
		return h.handleHubStatus()
	case "hub.shutdown":
		h.cancel()
		return HubResponse{Result: json.RawMessage(`"ok"`)}
	default:
		return HubResponse{Error: fmt.Sprintf("unknown method: %s", method)}
	}
}

func (h *Hub) handleSessionList() HubResponse {
	sessions := h.ListSessions()
	data, err := json.Marshal(sessions)
	if err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: data}
}

func (h *Hub) handleSessionCreate() HubResponse {
	ms, err := h.CreateAndStartSession(SourceHub)
	if err != nil {
		return HubResponse{Error: err.Error()}
	}
	data, err := json.Marshal(ms)
	if err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: data}
}

type sessionIDParams struct {
	SessionID string `json:"session_id"`
}

func (h *Hub) handleSessionDelete(params json.RawMessage) HubResponse {
	var p sessionIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return HubResponse{Error: err.Error()}
	}

	h.mu.Lock()
	ms, ok := h.sessions[p.SessionID]
	h.mu.Unlock()

	if !ok {
		return HubResponse{Error: "session not found"}
	}

	// If hub manages an engine for this session, kill it.
	if ms.EnginePID != 0 {
		h.mu.Lock()
		sid := p.SessionID
		pid := ms.EnginePID
		h.mu.Unlock()

		// Find the engine proc and kill it.
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Kill()
		}
		_ = sid
	}

	if err := h.store.Delete(h.ctx, p.SessionID); err != nil {
		return HubResponse{Error: err.Error()}
	}

	h.mu.Lock()
	delete(h.sessions, p.SessionID)
	h.mu.Unlock()

	return HubResponse{Result: json.RawMessage(`"ok"`)}
}

type sessionInputParams struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

func (h *Hub) handleSessionInput(params json.RawMessage) HubResponse {
	var p sessionInputParams
	if err := json.Unmarshal(params, &p); err != nil {
		return HubResponse{Error: err.Error()}
	}

	if err := h.SendInput(p.SessionID, p.Text); err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: json.RawMessage(`"ok"`)}
}

func (h *Hub) handleSessionCancel(params json.RawMessage) HubResponse {
	var p sessionIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return HubResponse{Error: err.Error()}
	}

	if err := h.CancelEngine(p.SessionID); err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: json.RawMessage(`"ok"`)}
}

func (h *Hub) handleSessionGet(params json.RawMessage) HubResponse {
	var p sessionIDParams
	if err := json.Unmarshal(params, &p); err != nil {
		return HubResponse{Error: err.Error()}
	}

	ms := h.GetSession(p.SessionID)
	if ms == nil {
		return HubResponse{Error: "session not found"}
	}

	data, err := json.Marshal(ms)
	if err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: data}
}

type HubStatus struct {
	Running      bool   `json:"running"`
	ProjectDir   string `json:"project_dir"`
	SocketPath   string `json:"socket_path"`
	ActiveCount  int    `json:"active_count"`
	SessionCount int    `json:"session_count"`
}

func (h *Hub) handleHubStatus() HubResponse {
	h.mu.RLock()
	count := len(h.sessions)
	active := 0
	for _, s := range h.sessions {
		if s.EnginePID != 0 {
			active++
		}
	}
	h.mu.RUnlock()

	status := HubStatus{
		Running:      true,
		ProjectDir:   h.projectDir,
		SocketPath:   h.socketPath,
		ActiveCount:  active,
		SessionCount: count,
	}

	data, err := json.Marshal(status)
	if err != nil {
		return HubResponse{Error: err.Error()}
	}
	return HubResponse{Result: data}
}
