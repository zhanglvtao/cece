package observatory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

func TestStoreDerivesToolTopology(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.ModelRequestStarted{Reason: "user", EstimatedInputTokens: 1000})
	store.Apply(protocol.StreamStarted{Model: "gpt-test", InputTokens: 1000})
	store.Apply(protocol.ToolExecStarted{ID: "1", Name: "Edit"})
	state := store.State()
	if phaseStatus(state, "tool_exec") != "active" {
		t.Fatalf("tool_exec phase = %q, want active", phaseStatus(state, "tool_exec"))
	}
	if nodeStatus(state, "tool:Edit") != "active" {
		t.Fatalf("tool node = %q, want active", nodeStatus(state, "tool:Edit"))
	}
}

func TestServerSnapshotPostUpdatesTUIState(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, hub, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartServer error = %v", err)
	}
	defer server.Close(context.Background())
	snapshot := protocol.ObservatorySnapshotEvent{
		Scope:      "tui:test",
		CapturedAt: time.Now(),
		Nodes:      []protocol.ObservatoryNode{{ID: "tui", Label: "TUI Client", Kind: "tui", Status: "active"}},
	}
	body, _ := json.Marshal(snapshot)
	resp, err := http.Post(server.Info().URL+"/api/snapshot", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST snapshot error = %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := nodeStatus(hub.State(), "tui"); got != "active" {
		t.Fatalf("tui status = %q, want active", got)
	}
}

func TestServerStateEndpoint(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, hub, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartServer error = %v", err)
	}
	defer server.Close(context.Background())
	server.PublishStarted()
	resp, err := http.Get(server.Info().URL + "/api/state")
	if err != nil {
		t.Fatalf("GET state error = %v", err)
	}
	defer resp.Body.Close()
	var state State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Server.URL != server.Info().URL {
		t.Fatalf("state URL = %q, want %q", state.Server.URL, server.Info().URL)
	}
}

func phaseStatus(state State, id string) string {
	for _, p := range state.Phases {
		if p.ID == id {
			return p.Status
		}
	}
	return ""
}

func nodeStatus(state State, id string) string {
	for _, n := range state.Nodes {
		if n.ID == id {
			return n.Status
		}
	}
	return ""
}
