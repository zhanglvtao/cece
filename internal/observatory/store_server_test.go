package observatory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

func TestServerIndexServesEmbeddedReactApp(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, hub, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartServer error = %v", err)
	}
	defer server.Close(context.Background())
	resp, err := http.Get(server.Info().URL + "/")
	if err != nil {
		t.Fatalf("GET index error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	text := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(text, `id="root"`) || !strings.Contains(text, "/assets/") {
		t.Fatalf("index did not look like embedded React app: %q", text)
	}
}

func TestServerServesEmbeddedAssets(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, hub, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartServer error = %v", err)
	}
	defer server.Close(context.Background())
	indexResp, err := http.Get(server.Info().URL + "/")
	if err != nil {
		t.Fatalf("GET index error = %v", err)
	}
	indexBody, err := io.ReadAll(indexResp.Body)
	_ = indexResp.Body.Close()
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	assetPath := firstAssetPath(string(indexBody))
	if assetPath == "" {
		t.Fatalf("missing asset path in index: %q", string(indexBody))
	}
	assetResp, err := http.Get(server.Info().URL + assetPath)
	if err != nil {
		t.Fatalf("GET asset error = %v", err)
	}
	defer assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", assetResp.StatusCode)
	}
	contentType := assetResp.Header.Get("Content-Type")
	if contentType == "" {
		t.Fatal("asset Content-Type is empty")
	}
}

func firstAssetPath(html string) string {
	start := strings.Index(html, "/assets/")
	if start < 0 {
		return ""
	}
	end := start
	for end < len(html) && html[end] != '"' && html[end] != '\'' && html[end] != '>' {
		end++
	}
	return html[start:end]
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
