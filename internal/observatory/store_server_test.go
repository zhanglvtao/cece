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
			"activity": "requesting model",
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
	// Progress events should not appear in the Outbox
	if len(agent.Outbox) != 0 {
		t.Fatalf("outbox = %+v, want empty (progress filtered)", agent.Outbox)
	}
}

func TestStoreRoutesSchedulerEventsToLifecycleLane(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.AgentBusEvent{
		MessageID: "agent-1-start",
		AgentID:   "agent-1",
		Kind:      "started",
		Lane:      "scheduler",
		StatusTo:  "starting",
		Payload:   map[string]any{"description": "explore repo"},
	})

	agent, ok := agentByID(store.State(), "agent-1")
	if !ok {
		t.Fatalf("missing agent-1: %+v", store.State().Agents)
	}
	if len(agent.Outbox) != 0 {
		t.Fatalf("outbox = %+v, want empty (scheduler must not pollute outbox)", agent.Outbox)
	}
	if len(agent.Lifecycle) != 1 || agent.Lifecycle[0].Kind != "started" {
		t.Fatalf("lifecycle = %+v, want one started item", agent.Lifecycle)
	}
}

func TestStoreProjectsRootOutboxSpawnEvent(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.AgentBusEvent{
		MessageID: "agent-1-spawn",
		AgentID:   "interactive-root",
		Kind:      "spawn",
		Lane:      "outbox",
		StatusTo:  "starting",
		Payload:   map[string]any{"target": "agent-1", "description": "explore repo"},
	})

	root, ok := agentByID(store.State(), "interactive-root")
	if !ok {
		t.Fatalf("missing interactive-root: %+v", store.State().Agents)
	}
	if len(root.Outbox) != 1 || root.Outbox[0].Kind != "spawn" {
		t.Fatalf("root outbox = %+v, want one spawn item", root.Outbox)
	}
	if root.Outbox[0].Payload["target"] != "agent-1" {
		t.Fatalf("root outbox payload = %+v", root.Outbox[0].Payload)
	}
}

func TestStoreBuildsRootTranscriptFromTurnFlow(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: "探索 cece 代码库"}})
	store.Apply(protocol.AssistantStarted{})
	store.Apply(protocol.AssistantDelta{Text: "好的，我"})
	store.Apply(protocol.AssistantDelta{Text: "先看结构"})
	store.Apply(protocol.StreamCompleted{OutputTokens: 10})
	store.Apply(protocol.ToolExecStarted{ID: "1", Name: "Grep"})
	store.Apply(protocol.ToolExecCompleted{ID: "1", Name: "Grep", Result: protocol.ToolResult{Content: "hits"}})

	root, ok := agentByID(store.State(), "interactive-root")
	if !ok {
		t.Fatalf("missing interactive-root: %+v", store.State().Agents)
	}
	tr := root.Transcript
	if len(tr) != 3 {
		t.Fatalf("transcript = %+v, want 3 items", tr)
	}
	if tr[0].Role != "user" || tr[0].Text != "探索 cece 代码库" {
		t.Fatalf("transcript[0] = %+v, want user message", tr[0])
	}
	if tr[1].Role != "assistant" || tr[1].Text != "好的，我先看结构" {
		t.Fatalf("transcript[1] = %+v, want assembled assistant text", tr[1])
	}
	if tr[2].Role != "tool" || tr[2].Tool != "Grep" || tr[2].Status != "ok" {
		t.Fatalf("transcript[2] = %+v, want tool item", tr[2])
	}
}

func TestStoreBuildsSubAgentTranscriptWithDedup(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "worker"})
	store.Apply(protocol.SubAgentActivityEvent{ID: "agent-1", Status: "running", ToolCall: "Grep pattern", LastAssistantMsg: "searching"})
	// Repeat the same activity — should be de-duplicated.
	store.Apply(protocol.SubAgentActivityEvent{ID: "agent-1", Status: "running", ToolCall: "Grep pattern", LastAssistantMsg: "searching"})
	store.Apply(protocol.SubAgentActivityEvent{ID: "agent-1", Status: "running", ToolCall: "Read file.go", LastAssistantMsg: "reading"})

	agent, ok := agentByID(store.State(), "agent-1")
	if !ok {
		t.Fatalf("missing agent-1: %+v", store.State().Agents)
	}
	// 2 unique tool calls + 2 unique assistant messages = 4 items.
	if len(agent.Transcript) != 4 {
		t.Fatalf("transcript = %+v, want 4 de-duplicated items", agent.Transcript)
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

func TestStoreProjectsRootInboxAgentBusEvent(t *testing.T) {
	store := NewStore()
	store.Apply(protocol.AgentBusEvent{
		MessageID: "agent-1-result-root-inbox",
		TraceID:   "agent-1",
		AgentID:   "interactive-root",
		Kind:      "result",
		Lane:      "inbox",
		StatusTo:  "completed",
		Payload: map[string]any{
			"agent_id":    "agent-1",
			"summary":     "done",
			"result_path": "/tmp/result.txt",
		},
	})

	state := store.State()
	if len(state.Agents) == 0 || state.Agents[0].ID != "interactive-root" {
		t.Fatalf("interactive-root should stay first: %+v", state.Agents)
	}
	root, ok := agentByID(state, "interactive-root")
	if !ok {
		t.Fatalf("missing interactive-root: %+v", state.Agents)
	}
	if root.Status != "completed" {
		t.Fatalf("root status = %q, want completed", root.Status)
	}
	if len(root.Inbox) != 1 {
		t.Fatalf("root inbox = %+v, want one item", root.Inbox)
	}
	item := root.Inbox[0]
	if item.MessageID != "agent-1-result-root-inbox" || item.Kind != "result" || item.Lane != "inbox" || item.StatusTo != "completed" {
		t.Fatalf("root inbox item = %+v", item)
	}
	if item.Payload["agent_id"] != "agent-1" || item.Payload["result_path"] != "/tmp/result.txt" || item.Payload["summary"] != "done" {
		t.Fatalf("root inbox payload = %+v", item.Payload)
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

func TestServerStateEndpointIncludesFullObservatoryState(t *testing.T) {
	hub := NewHub()
	hub.Observe(protocol.ObservatorySnapshotEvent{
		Scope:       "tui:test",
		Version:     3,
		CapturedAt:  time.Now(),
		ActivePhase: "model_stream",
		Nodes:       []protocol.ObservatoryNode{{ID: "custom-node", Label: "Custom Node", Kind: "test", Status: "active"}},
		Edges:       []protocol.ObservatoryEdge{{From: "custom-node", To: "engine", Label: "observes", Status: "active"}},
		Phases:      []protocol.ObservatoryPhase{{ID: "custom_phase", Label: "custom_phase", Status: "active"}},
		Metrics:     []protocol.ObservatoryMetric{{Name: "custom_metric", Value: "42"}},
		Evidence:    []string{"snapshot evidence"},
	})
	hub.Observe(protocol.AgentBusEvent{MessageID: "progress-1", AgentID: "agent-1", Kind: "progress", Lane: "outbox", StatusTo: "running", Payload: map[string]any{"activity": "reading files"}})
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
	if state.Snapshots["tui:test"].Version != 3 {
		t.Fatalf("snapshots = %+v", state.Snapshots)
	}
	if nodeStatus(state, "custom-node") != "active" {
		t.Fatalf("custom node status = %q", nodeStatus(state, "custom-node"))
	}
	if !hasEdge(state, "custom-node", "engine", "observes") {
		t.Fatalf("missing custom edge: %+v", state.Edges)
	}
	if phaseStatus(state, "custom_phase") != "active" {
		t.Fatalf("custom phase = %q", phaseStatus(state, "custom_phase"))
	}
	if metricValue(state, "custom_metric") != "42" {
		t.Fatalf("custom metric = %q", metricValue(state, "custom_metric"))
	}
	if !strings.Contains(evidenceDetail(state, "ObservatorySnapshot"), "snapshot evidence") {
		t.Fatalf("snapshot evidence detail = %q", evidenceDetail(state, "ObservatorySnapshot"))
	}
	agent, ok := agentByID(state, "agent-1")
	if !ok || len(agent.Outbox) != 0 {
		t.Fatalf("agent projection = %+v, ok=%v (progress events filtered from outbox)", agent, ok)
	}
}

func TestHubRecordsBackpressureSubscriberDrop(t *testing.T) {
	hub := NewHub()
	_, cancel := hub.Subscribe()
	defer cancel()

	for i := 0; i < subscriberBuffer+1; i++ {
		hub.Observe(protocol.ModelRequestStarted{Reason: "test"})
	}

	state := hub.State()
	if state.Subscribers != 0 {
		t.Fatalf("subscribers = %d, want 0 after drop", state.Subscribers)
	}
	if metricValue(state, "subscriber_drops") != "1" {
		t.Fatalf("subscriber_drops metric = %q", metricValue(state, "subscriber_drops"))
	}
	if got := evidenceText(state, "SubscriberDrop"); !strings.Contains(got, "backpressure") {
		t.Fatalf("SubscriberDrop evidence = %q", got)
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

func metricValue(state State, name string) string {
	for _, metric := range state.Metrics {
		if metric.Name == name {
			return metric.Value
		}
	}
	return ""
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
