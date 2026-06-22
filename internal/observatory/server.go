package observatory

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

type Server struct {
	hub    *Hub
	server *http.Server
	ln     net.Listener
	info   ServerInfo
}

func StartServer(ctx context.Context, hub *Hub, host string, port int) (*Server, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	info := ServerInfo{Host: host, Port: addr.Port, URL: fmt.Sprintf("http://%s:%d", host, addr.Port)}
	s := &Server{hub: hub, ln: ln, info: info}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/health", s.handleHealth)
	s.server = &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()
	go func() { _ = s.server.Serve(ln) }()
	return s, nil
}

func (s *Server) Info() ServerInfo { return s.info }

func (s *Server) Close(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.hub.State())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": s.info.URL})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var snapshot protocol.ObservatorySnapshotEvent
	if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
		http.Error(w, "invalid snapshot", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(snapshot.Scope, "tui:") {
		http.Error(w, "snapshot scope must start with tui:", http.StatusBadRequest)
		return
	}
	s.hub.ObserveSnapshot(snapshot)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.hub.Subscribe()
	defer cancel()
	s.writeSSE(w, "state", map[string]any{"state": s.hub.State()})
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			s.writeSSE(w, "event", map[string]any{"kind": eventKind(ev), "event": ev, "state": s.hub.State()})
			flusher.Flush()
		}
	}
}

func (s *Server) writeSSE(w http.ResponseWriter, event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func (s *Server) PublishStarted() {
	info := s.Info()
	s.hub.Observe(protocol.ObservatoryServerStartedEvent{URL: info.URL, Host: info.Host, Port: info.Port})
}
