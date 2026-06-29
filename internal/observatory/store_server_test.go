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

func TestStoreTracksSimpleAgentMailboxState(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "worker"})
	store.Apply(protocol.AgentBusEvent{
		MessageID: "cmd-1",
		TraceID:   "agent-1",
		AgentID:   "agent-1",
		Kind:      "send_input",
		Lane:      "inbox",
		StatusTo:  "running",
		Payload: map[string]any{
			"summary": "send input",
		},
	})
	store.Apply(protocol.AgentBusEvent{
		MessageID: "evt-1",
		TraceID:   "agent-1",
		AgentID:   "agent-1",
		Kind:      "progress",
		Lane:      "outbox",
		StatusTo:  "running",
		Payload: map[string]any{
			"summary": "requesting model",
		},
	})

	state := store.State()
	agent, ok := agentByID(state, "agent-1")
	if !ok {
		t.Fatalf("missing agent-1: %+v", state.Agents)
	}
	if agent.Status != "running" {
		t.Fatalf("agent = %+v", agent)
	}
	if len(agent.Inbox) != 1 || agent.Inbox[0].MessageID != "cmd-1" {
		t.Fatalf("inbox = %+v", agent.Inbox)
	}
	if len(agent.Outbox) != 1 || agent.Outbox[0].MessageID != "evt-1" {
		t.Fatalf("outbox = %+v", agent.Outbox)
	}
}

func TestStoreInitialStateIncludesInteractiveRootAgent(t *testing.T) {
	store := NewStore()
	state := store.State()

	agent, ok := agentByID(state, "interactive-root")
	if !ok {
		t.Fatalf("missing interactive-root agent: %+v", state.Agents)
	}
	if agent.Description != "Current Agent" {
		t.Fatalf("description = %q, want Current Agent", agent.Description)
	}
	if agent.Status != "idle" {
		t.Fatalf("status = %q, want idle", agent.Status)
	}
	if len(state.Agents) == 0 || state.Agents[0].ID != "interactive-root" {
		t.Fatalf("interactive-root should be first for default selection: %+v", state.Agents)
	}
}

func TestStoreUpdatesInteractiveRootAgentLifecycle(t *testing.T) {
	store := NewStore()

	store.Apply(protocol.ModelRequestStarted{Reason: "user", EstimatedInputTokens: 1000})
	state := store.State()
	agent, ok := agentByID(state, "interactive-root")
	if !ok {
		t.Fatal("missing interactive-root after model request")
	}
	if agent.Status != "running" {
		t.Fatalf("status after ModelRequestStarted = %q, want running", agent.Status)
	}

	store.Apply(protocol.TurnCompleted{})
	state = store.State()
	agent, _ = agentByID(state, "interactive-root")
	if agent.Status != "idle" {
		t.Fatalf("status after TurnCompleted = %q, want idle", agent.Status)
	}

	store.Apply(protocol.RunFailed{})
	state = store.State()
	agent, _ = agentByID(state, "interactive-root")
	if agent.Status != "failed" {
		t.Fatalf("status after RunFailed = %q, want failed", agent.Status)
	}
}

func TestStoreSortsInteractiveRootBeforeSubAgents(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "worker"})

	state := store.State()
	if len(state.Agents) < 2 {
		t.Fatalf("agents = %+v, want root and sub-agent", state.Agents)
	}
	if state.Agents[0].ID != "interactive-root" {
		t.Fatalf("first agent = %q, want interactive-root", state.Agents[0].ID)
	}
}

func TestStoreSkeletonSeparatesControlPathFromTelemetry(t *testing.T) {
	store := NewStore()
	state := store.State()
	if !hasEdge(state, "runtime", "engine", "turn request") {
		t.Fatal("missing runtime -> engine control edge")
	}
	if hasEdge(state, "hub", "engine", "all events") {
		t.Fatal("hub -> engine edge should not be in control path")
	}
	if !hasEdge(state, "runtime", "hub", "telemetry") || !hasEdge(state, "engine", "hub", "telemetry") {
		t.Fatal("missing telemetry edges into observatory hub")
	}
}

func TestStoreEvidenceSummariesAreReadable(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.StreamEventDetail{EventType: "message_start"})
	store.Apply(protocol.StreamEventDetail{EventType: "content_block_delta", Detail: "input_json_delta", Text: `{"path":"main.go"}`})
	store.Apply(protocol.ToolCallDelta{ID: "1", Delta: `{"old`})
	store.Apply(protocol.ToolCallCompleted{ID: "1", Name: "Edit", Input: json.RawMessage(`{"path":"main.go","old_string":"a","new_string":"b"}`)})
	store.Apply(protocol.ToolExecCompleted{ID: "1", Name: "Edit", Result: protocol.ToolResult{Content: "patched main.go"}})
	state := store.State()
	if evidenceText(state, "StreamEventDetail") == "StreamEventDetail" {
		t.Fatal("stream detail evidence fell back to event type")
	}
	if got := evidenceTextContains(state, "StreamEventDetail", "content_block_delta"); got != "" {
		t.Fatalf("delta stream evidence = %q, want filtered", got)
	}
	if got := evidenceText(state, "ToolCallDelta"); got != "" {
		t.Fatalf("ToolCallDelta evidence = %q, want filtered", got)
	}
	if got := evidenceText(state, "ToolCallCompleted"); !strings.Contains(got, "Edit") {
		t.Fatalf("ToolCallCompleted evidence = %q, want tool name", got)
	}
	if got := evidenceDetail(state, "ToolCallCompleted"); !strings.Contains(got, "main.go") {
		t.Fatalf("ToolCallCompleted detail = %q, want input detail", got)
	}
	if got := evidenceText(state, "ToolExecCompleted"); !strings.Contains(got, "ok") {
		t.Fatalf("ToolExecCompleted evidence = %q, want status", got)
	}
	if got := evidenceDetail(state, "ToolExecCompleted"); got != "patched main.go" {
		t.Fatalf("ToolExecCompleted detail = %q", got)
	}
	store.Apply(protocol.CompletionGateEvaluated{
		Attempt:     1,
		MaxAttempts: 3,
		Status:      protocol.CompletionGateBlocked,
		Next:        "continue",
		Checks:      []protocol.CompletionGateCheck{{Name: "TodoGate", Status: protocol.CompletionGateBlocked}},
	})
	if got := evidenceText(state, "CompletionGateEvaluated"); got != "" {
		t.Fatalf("test setup saw stale state evidence = %q", got)
	}
	state = store.State()
	if got := evidenceText(state, "CompletionGateEvaluated"); !strings.Contains(got, "completion gate blocked") || !strings.Contains(got, "TodoGate=blocked") {
		t.Fatalf("CompletionGateEvaluated evidence = %q", got)
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

func TestServerStateIncludesAgents(t *testing.T) {
	hub := NewHub()
	hub.Observe(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "worker"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, hub, "127.0.0.1", 0)
	if err != nil {
		t.Fatalf("StartServer error = %v", err)
	}
	defer server.Close(context.Background())

	resp, err := http.Get(server.Info().URL + "/api/state")
	if err != nil {
		t.Fatalf("GET /api/state error = %v", err)
	}
	defer resp.Body.Close()

	var state State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state error = %v", err)
	}
	if _, ok := agentByID(state, "agent-1"); !ok {
		t.Fatalf("missing agent-1: %+v", state.Agents)
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

func agentByID(state State, id string) (AgentView, bool) {
	for _, agent := range state.Agents {
		if agent.ID == id {
			return agent, true
		}
	}
	return AgentView{}, false
}

func nodeStatus(state State, id string) string {
	for _, n := range state.Nodes {
		if n.ID == id {
			return n.Status
		}
	}
	return ""
}

func hasEdge(state State, from, to, label string) bool {
	for _, edge := range state.Edges {
		if edge.From == from && edge.To == to && edge.Label == label {
			return true
		}
	}
	return false
}

func evidenceText(state State, kind string) string {
	for _, item := range state.Evidence {
		if item.Kind == kind {
			return item.Text
		}
	}
	return ""
}

func evidenceDetail(state State, kind string) string {
	for _, item := range state.Evidence {
		if item.Kind == kind {
			return item.Detail
		}
	}
	return ""
}

func evidenceTextContains(state State, kind, text string) string {
	for _, item := range state.Evidence {
		if item.Kind == kind && strings.Contains(item.Text, text) {
			return item.Text
		}
	}
	return ""
}
