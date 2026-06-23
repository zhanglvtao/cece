package observatory

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

const maxEvidence = 80

type ServerInfo struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type EvidenceItem struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	Detail string    `json:"detail,omitempty"`
}

type State struct {
	Server      ServerInfo                                   `json:"server"`
	UpdatedAt   time.Time                                    `json:"updated_at"`
	Subscribers int                                          `json:"subscribers"`
	Snapshots   map[string]protocol.ObservatorySnapshotEvent `json:"snapshots"`
	Nodes       []protocol.ObservatoryNode                   `json:"nodes"`
	Edges       []protocol.ObservatoryEdge                   `json:"edges"`
	Phases      []protocol.ObservatoryPhase                  `json:"phases"`
	Metrics     []protocol.ObservatoryMetric                 `json:"metrics"`
	Evidence    []EvidenceItem                               `json:"evidence"`
}

type Store struct {
	mu          sync.Mutex
	server      ServerInfo
	updatedAt   time.Time
	subscribers int
	snapshots   map[string]protocol.ObservatorySnapshotEvent
	nodes       map[string]protocol.ObservatoryNode
	edges       map[string]protocol.ObservatoryEdge
	phases      map[string]protocol.ObservatoryPhase
	metrics     map[string]protocol.ObservatoryMetric
	evidence    []EvidenceItem
}

func NewStore() *Store {
	s := &Store{
		snapshots: make(map[string]protocol.ObservatorySnapshotEvent),
		nodes:     make(map[string]protocol.ObservatoryNode),
		edges:     make(map[string]protocol.ObservatoryEdge),
		phases:    make(map[string]protocol.ObservatoryPhase),
		metrics:   make(map[string]protocol.ObservatoryMetric),
	}
	s.ensureSkeletonLocked()
	return s
}

func (s *Store) Apply(ev protocol.Event) {
	if ev == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.updatedAt = now
	s.ensureSkeletonLocked()
	if text, detail, ok := eventEvidence(ev); ok {
		s.addEvidenceLocked(now, eventKind(ev), text, detail)
	}

	switch e := ev.(type) {
	case protocol.ObservatoryServerStartedEvent:
		s.server = ServerInfo{URL: e.URL, Host: e.Host, Port: e.Port}
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "hub", Label: "Observatory Hub", Kind: "hub", Status: "active", Meta: map[string]string{"url": e.URL}})
	case protocol.ObservatorySnapshotEvent:
		s.snapshots[e.Scope] = cloneSnapshot(e)
		for _, n := range e.Nodes {
			s.upsertNodeLocked(n)
		}
		for _, edge := range e.Edges {
			s.upsertEdgeLocked(edge)
		}
		for _, p := range e.Phases {
			s.setPhaseLocked(p.ID, p.Label, p.Status)
		}
		for _, m := range e.Metrics {
			s.metrics[m.Name] = m
		}
	case protocol.UserMessageAdded:
		s.setPhaseLocked("user_input", "user_input", "done")
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "user", To: "tui", Label: "message", Status: "done"})
	case protocol.ModelRequestStarted:
		s.setPhaseLocked("prompt_build", "prompt_build", "done")
		s.setPhaseLocked("model_request", "model_request", "active")
		s.setPhaseLocked("model_stream", "model_stream", "idle")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "engine", Label: "Engine", Kind: "engine", Status: "active"})
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "model", Label: "Model", Kind: "model", Status: "active"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: "model", Label: "request", Status: "active"})
		if e.EstimatedInputTokens > 0 {
			s.metrics["input_tokens"] = protocol.ObservatoryMetric{Name: "input_tokens", Value: formatTokenK(e.EstimatedInputTokens)}
		}
		if e.ContextWindow > 0 {
			s.metrics["context_window"] = protocol.ObservatoryMetric{Name: "context_window", Value: formatTokenK(e.ContextWindow)}
		}
		if e.APICalls > 0 {
			s.metrics["api_calls"] = protocol.ObservatoryMetric{Name: "api_calls", Value: fmt.Sprintf("%d", e.APICalls)}
		}
	case protocol.StreamStarted:
		s.setPhaseLocked("model_request", "model_request", "done")
		s.setPhaseLocked("model_stream", "model_stream", "active")
		label := "Model"
		if e.Model != "" {
			label = e.Model
			s.metrics["model"] = protocol.ObservatoryMetric{Name: "model", Value: e.Model}
		}
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "model", Label: label, Kind: "model", Status: "active"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "model", To: "engine", Label: "stream", Status: "active"})
		if e.InputTokens > 0 {
			s.metrics["input_tokens"] = protocol.ObservatoryMetric{Name: "input_tokens", Value: formatTokenK(e.InputTokens)}
		}
	case protocol.AssistantStarted, protocol.AssistantDelta:
		s.setPhaseLocked("model_stream", "model_stream", "active")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "model", Label: "Model", Kind: "model", Status: "active"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "model", To: "engine", Label: "stream", Status: "active"})
	case protocol.StreamCompleted:
		s.setPhaseLocked("model_stream", "model_stream", "done")
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "model", To: "engine", Label: "stream", Status: "done"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: "model", Label: "request", Status: "done"})
		if e.OutputTokens > 0 {
			s.metrics["output_tokens"] = protocol.ObservatoryMetric{Name: "output_tokens", Value: formatTokenK(e.OutputTokens)}
		}
		for _, name := range e.ToolCalls {
			id := toolNodeID(name)
			s.upsertNodeLocked(protocol.ObservatoryNode{ID: id, Label: "Tool: " + name, Kind: "tool", Status: "waiting"})
			s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: id, Label: "tool", Status: "waiting"})
		}
	case protocol.ToolExecStarted:
		s.setPhaseLocked("tool_exec", "tool_exec", "active")
		id := toolNodeID(e.Name)
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: id, Label: "Tool: " + e.Name, Kind: "tool", Status: "active"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: id, Label: "tool exec", Status: "active"})
	case protocol.ToolExecCompleted:
		status := "done"
		if e.Result.IsError {
			status = "failed"
		}
		s.setPhaseLocked("tool_exec", "tool_exec", status)
		id := toolNodeID(e.Name)
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: id, Label: "Tool: " + e.Name, Kind: "tool", Status: status})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: id, Label: "tool exec", Status: status})
	case protocol.SubAgentStartedEvent:
		s.setPhaseLocked("subagents", "subagents", "active")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "orchestrator", Label: "Orchestrator", Kind: "orchestrator", Status: "active"})
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: e.ID, Label: agentLabel(e.ID, e.Description), Kind: "agent", Status: "active"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "orchestrator", To: e.ID, Label: "spawn", Status: "active"})
	case protocol.SubAgentActivityEvent:
		status := agentStatus(e.Status)
		s.setPhaseLocked("subagents", "subagents", status)
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "orchestrator", Label: "Orchestrator", Kind: "orchestrator", Status: "active"})
		meta := map[string]string{}
		if e.Activity != "" {
			meta["activity"] = e.Activity
		}
		if e.Model != "" {
			meta["model"] = e.Model
		}
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: e.ID, Label: e.ID, Kind: "agent", Status: status, Meta: meta})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "orchestrator", To: e.ID, Label: "run", Status: status})
	case protocol.SubAgentCompletedEvent:
		s.setPhaseLocked("subagents", "subagents", "done")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: e.ID, Label: agentLabel(e.ID, e.Description), Kind: "agent", Status: "done"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "orchestrator", To: e.ID, Label: "run", Status: "done"})
	case protocol.SubAgentFailedEvent:
		s.setPhaseLocked("subagents", "subagents", "failed")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: e.ID, Label: agentLabel(e.ID, e.Description), Kind: "agent", Status: "failed"})
		s.upsertEdgeLocked(protocol.ObservatoryEdge{From: "orchestrator", To: e.ID, Label: "run", Status: "failed"})
	case protocol.TurnCompleted:
		s.setPhaseLocked("done", "done", "done")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "engine", Label: "Engine", Kind: "engine", Status: "done"})
	case protocol.RunFailed:
		s.setPhaseLocked("done", "done", "failed")
		s.upsertNodeLocked(protocol.ObservatoryNode{ID: "engine", Label: "Engine", Kind: "engine", Status: "failed"})
	}
}

func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *Store) SetSubscriberCount(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = n
}

func (s *Store) stateLocked() State {
	nodes := make([]protocol.ObservatoryNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		nodes = append(nodes, cloneNode(n))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	edges := make([]protocol.ObservatoryEdge, 0, len(s.edges))
	for _, e := range s.edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edgeKey(edges[i]) < edgeKey(edges[j]) })

	phases := make([]protocol.ObservatoryPhase, 0, len(s.phases))
	for _, p := range s.phases {
		phases = append(phases, p)
	}
	sort.SliceStable(phases, func(i, j int) bool { return phaseOrder(phases[i].ID) < phaseOrder(phases[j].ID) })

	metrics := make([]protocol.ObservatoryMetric, 0, len(s.metrics))
	for _, m := range s.metrics {
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Name < metrics[j].Name })

	snapshots := make(map[string]protocol.ObservatorySnapshotEvent, len(s.snapshots))
	for k, v := range s.snapshots {
		snapshots[k] = cloneSnapshot(v)
	}
	evidence := make([]EvidenceItem, len(s.evidence))
	copy(evidence, s.evidence)
	return State{
		Server:      s.server,
		UpdatedAt:   s.updatedAt,
		Subscribers: s.subscribers,
		Snapshots:   snapshots,
		Nodes:       nodes,
		Edges:       edges,
		Phases:      phases,
		Metrics:     metrics,
		Evidence:    evidence,
	}
}

func (s *Store) ensureSkeletonLocked() {
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "user", Label: "User", Kind: "user", Status: "idle"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "tui", Label: "TUI Client", Kind: "tui", Status: "idle"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "runtime", Label: "Runtime Host", Kind: "runtime", Status: "active"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "hub", Label: "Observatory Hub", Kind: "hub", Status: "active"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "engine", Label: "Engine", Kind: "engine", Status: "idle"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "model", Label: "Model", Kind: "model", Status: "idle"})
	s.ensureNodeLocked(protocol.ObservatoryNode{ID: "orchestrator", Label: "Orchestrator", Kind: "orchestrator", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "user", To: "tui", Label: "message", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "tui", To: "runtime", Label: "input action", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "runtime", To: "engine", Label: "turn request", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "runtime", To: "hub", Label: "telemetry", Status: "active"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: "hub", Label: "telemetry", Status: "active"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: "model", Label: "request", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "model", To: "engine", Label: "stream", Status: "idle"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "engine", To: "orchestrator", Label: "spawn", Status: "idle"})
	for _, p := range []protocol.ObservatoryPhase{
		{ID: "user_input", Label: "user_input", Status: "idle"},
		{ID: "prompt_build", Label: "prompt_build", Status: "idle"},
		{ID: "model_request", Label: "model_request", Status: "idle"},
		{ID: "model_stream", Label: "model_stream", Status: "idle"},
		{ID: "tool_exec", Label: "tool_exec", Status: "idle"},
		{ID: "subagents", Label: "subagents", Status: "idle"},
		{ID: "done", Label: "done", Status: "idle"},
	} {
		if _, ok := s.phases[p.ID]; !ok {
			s.phases[p.ID] = p
		}
	}
}

func (s *Store) ensureNodeLocked(n protocol.ObservatoryNode) {
	if n.ID == "" {
		return
	}
	if _, ok := s.nodes[n.ID]; ok {
		return
	}
	s.upsertNodeLocked(n)
}

func (s *Store) ensureEdgeLocked(e protocol.ObservatoryEdge) {
	if e.From == "" || e.To == "" {
		return
	}
	if _, ok := s.edges[edgeKey(e)]; ok {
		return
	}
	s.upsertEdgeLocked(e)
}

func (s *Store) upsertNodeLocked(n protocol.ObservatoryNode) {
	if n.ID == "" {
		return
	}
	if n.Label == "" {
		n.Label = n.ID
	}
	if n.Status == "" {
		n.Status = "idle"
	}
	if existing, ok := s.nodes[n.ID]; ok {
		if n.Kind == "" {
			n.Kind = existing.Kind
		}
		if len(n.Meta) == 0 {
			n.Meta = existing.Meta
		}
	}
	s.nodes[n.ID] = cloneNode(n)
}

func (s *Store) upsertEdgeLocked(e protocol.ObservatoryEdge) {
	if e.From == "" || e.To == "" {
		return
	}
	if e.Status == "" {
		e.Status = "idle"
	}
	s.edges[edgeKey(e)] = e
}

func (s *Store) setPhaseLocked(id, label, status string) {
	if id == "" {
		return
	}
	if label == "" {
		label = id
	}
	if status == "" {
		status = "idle"
	}
	s.phases[id] = protocol.ObservatoryPhase{ID: id, Label: label, Status: status}
}

func (s *Store) addEvidenceLocked(t time.Time, kind, text, detail string) {
	if text == "" {
		text = kind
	}
	s.evidence = append(s.evidence, EvidenceItem{Time: t, Kind: kind, Text: truncate(text, 240), Detail: truncate(detail, 1200)})
	if len(s.evidence) > maxEvidence {
		s.evidence = append([]EvidenceItem(nil), s.evidence[len(s.evidence)-maxEvidence:]...)
	}
}

func eventKind(ev protocol.Event) string {
	t := reflect.TypeOf(ev)
	if t == nil {
		return "unknown"
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	name = strings.TrimSuffix(name, "Event")
	return name
}

func eventEvidence(ev protocol.Event) (string, string, bool) {
	summary := eventSummary(ev)
	if summary == "" {
		return "", "", false
	}
	return summary, eventDetail(ev), true
}

func eventSummary(ev protocol.Event) string {
	switch e := ev.(type) {
	case protocol.ObservatoryServerStartedEvent:
		return "observatory server " + e.URL
	case protocol.ObservatorySnapshotEvent:
		return "snapshot " + e.Scope
	case protocol.EngineReadyEvent:
		return "engine ready " + e.Model
	case protocol.SessionCreated:
		return "session created " + nonEmpty(e.Title, e.ID)
	case protocol.UserMessageAdded:
		return "user message " + firstLine(e.Message.Content)
	case protocol.SystemReminderAdded:
		return "system reminder " + firstLine(e.Content)
	case protocol.ModelRequestStarted:
		parts := []string{"model request", e.Reason}
		if e.EstimatedInputTokens > 0 {
			parts = append(parts, fmt.Sprintf("input=%s", formatTokenK(e.EstimatedInputTokens)))
		}
		if e.ContextWindow > 0 {
			parts = append(parts, fmt.Sprintf("ctx=%s", formatTokenK(e.ContextWindow)))
		}
		if len(e.ToolResults) > 0 {
			parts = append(parts, "tools="+strings.Join(e.ToolResults, ","))
		}
		return strings.Join(parts, " ")
	case protocol.StreamStarted:
		parts := []string{"stream started"}
		if e.Model != "" {
			parts = append(parts, e.Model)
		}
		if e.InputTokens > 0 {
			parts = append(parts, fmt.Sprintf("input=%s", formatTokenK(e.InputTokens)))
		}
		if len(e.Tools) > 0 {
			parts = append(parts, fmt.Sprintf("tools=%d", len(e.Tools)))
		}
		return strings.Join(parts, " ")
	case protocol.StreamEventDetail:
		if e.EventType == "content_block_delta" || (e.EventType == "message_delta" && e.Detail != "stop_reason") {
			return ""
		}
		if e.Text != "" {
			return "stream " + compactJoin(e.EventType, e.Detail, firstLine(e.Text))
		}
		return "stream " + compactJoin(e.EventType, e.Detail)
	case protocol.AssistantStarted:
		return "assistant started"
	case protocol.AssistantDelta:
		return ""
	case protocol.AssistantCompleted:
		return "assistant completed " + e.Duration.String()
	case protocol.StreamCompleted:
		parts := []string{"stream completed"}
		if e.StopReason != "" {
			parts = append(parts, e.StopReason)
		}
		if e.OutputTokens > 0 {
			parts = append(parts, fmt.Sprintf("output=%s", formatTokenK(e.OutputTokens)))
		}
		if len(e.ToolCalls) > 0 {
			parts = append(parts, "tool_use="+strings.Join(e.ToolCalls, ","))
		}
		return strings.Join(parts, " ")
	case protocol.TruncationRetry:
		return fmt.Sprintf("truncation retry attempt=%d max_tokens %d→%d", e.Attempt, e.PrevMaxTokens, e.NewMaxTokens)
	case protocol.ToolCallStarted:
		return "tool call started " + e.Name
	case protocol.ToolCallDelta:
		return ""
	case protocol.ToolCallCompleted:
		return "tool call completed " + e.Name
	case protocol.ToolCallsReady:
		return fmt.Sprintf("tool calls ready %d", len(e.Calls))
	case protocol.ToolExecStarted:
		return "tool started " + e.Name
	case protocol.ToolExecDelta:
		return ""
	case protocol.ToolExecCompleted:
		status := "ok"
		if e.Result.IsError {
			status = "error"
		}
		return "tool completed " + e.Name + " " + status
	case protocol.ThinkingStarted:
		return fmt.Sprintf("thinking started block=%d", e.Index)
	case protocol.ThinkingDelta:
		return ""
	case protocol.ThinkingCompleted:
		return "thinking completed " + firstLine(e.Text)
	case protocol.PlanApprovalRequested:
		return "plan approval requested " + e.PlanFile
	case protocol.PlanRejected:
		return "plan rejected"
	case protocol.ToolCallsRejected:
		return "tool calls rejected"
	case protocol.QuestionAsked:
		return fmt.Sprintf("question asked %d", len(e.Questions))
	case protocol.QueuedInputPromoted:
		return "queued input promoted"
	case protocol.CompactingEvent:
		return "compacting history"
	case protocol.CompactedEvent:
		if e.Err != "" {
			return "compact failed " + e.Err
		}
		return fmt.Sprintf("compacted tokens %s→%s messages %d→%d", formatTokenK(e.TokensBefore), formatTokenK(e.TokensAfter), e.MessagesBefore, e.MessagesAfter)
	case protocol.TruncatedToolResultsEvent:
		return fmt.Sprintf("truncated tool results %d tokens %s→%s", e.TruncatedCount, formatTokenK(e.TokensBefore), formatTokenK(e.TokensAfter))
	case protocol.PrunedEvent:
		return fmt.Sprintf("pruned %d turns tokens %s→%s", e.PrunedTurns, formatTokenK(e.TokensBefore), formatTokenK(e.TokensAfter))
	case protocol.ContextNudgedEvent:
		return fmt.Sprintf("context nudge %d%% %s/%s", e.ContextPct, formatTokenK(e.ContextUsed), formatTokenK(e.ContextWindow))
	case protocol.TurnCompleted:
		return fmt.Sprintf("turn completed #%d last=%s total=%s/%s", e.TurnCount, formatTokenK(e.LastInputTokens), formatTokenK(e.TotalInputTokens), formatTokenK(e.TotalOutputTokens))
	case protocol.SessionTitleGeneratedEvent:
		if e.Err != "" {
			return "session title failed " + e.Err
		}
		return "session title " + e.Title
	case protocol.SessionDeletedEvent:
		return "session deleted " + e.SessionID
	case protocol.ModelsLoadedEvent:
		if e.Err != "" {
			return "models load failed " + e.Err
		}
		return fmt.Sprintf("models loaded %d", len(e.Models))
	case protocol.ModeChangedEvent:
		return "mode changed " + string(e.Mode)
	case protocol.EffortChangedEvent:
		return "effort changed " + e.Effort
	case protocol.ModeEvent:
		return "mode " + string(e.Mode)
	case protocol.HistoryClearedEvent:
		return "history cleared"
	case protocol.SessionLoadedEvent:
		if e.Err != "" {
			return "session load failed " + e.Err
		}
		return "session loaded " + e.SessionID
	case protocol.MCPServersListedEvent:
		return fmt.Sprintf("mcp servers listed %d", len(e.Servers))
	case protocol.MCPServerStatusChangedEvent:
		status := "disconnected"
		if e.Connected {
			status = "connected"
		}
		return "mcp " + e.Name + " " + status
	case protocol.TodoUpdatedEvent:
		return fmt.Sprintf("todo updated %d", len(e.Tasks))
	case protocol.SubAgentStartedEvent:
		return "subagent started " + compactJoin(e.ID, e.Description)
	case protocol.SubAgentActivityEvent:
		return "subagent activity " + compactJoin(e.ID, e.Status, e.Activity, e.ToolCall, e.LastAssistantMsg)
	case protocol.SubAgentCompletedEvent:
		return fmt.Sprintf("subagent completed %s turns=%d tokens=%s/%s", e.ID, e.TurnsUsed, formatTokenK(e.InputTokens), formatTokenK(e.OutputTokens))
	case protocol.SubAgentFailedEvent:
		return "subagent failed " + compactJoin(e.ID, e.Error)
	case protocol.ToolsListedEvent:
		return fmt.Sprintf("tools listed %d", len(e.Tools))
	case protocol.StatsEvent:
		return fmt.Sprintf("stats turns=%d calls=%d tokens=%s/%s", e.Stats.TurnCount, e.Stats.APICalls, formatTokenK(e.Stats.TotalInputTokens), formatTokenK(e.Stats.TotalOutputTokens))
	case protocol.RunFailed:
		return "run failed " + e.Err
	default:
		return eventKind(ev)
	}
}

func eventDetail(ev protocol.Event) string {
	switch e := ev.(type) {
	case protocol.ToolCallCompleted:
		return string(e.Input)
	case protocol.ToolCallsReady:
		return marshalDetail(e.Calls)
	case protocol.ToolExecCompleted:
		return e.Result.Content
	case protocol.ToolExecDelta:
		return e.Text
	case protocol.AssistantDelta:
		return e.Text
	case protocol.ThinkingCompleted:
		return e.Text
	case protocol.ThinkingDelta:
		return e.Text
	case protocol.CompactedEvent:
		return e.Summary
	case protocol.QuestionAsked:
		return marshalDetail(e.Questions)
	case protocol.ObservatorySnapshotEvent:
		return strings.Join(e.Evidence, "\n")
	case protocol.StreamStarted:
		return strings.Join(e.Tools, ", ")
	case protocol.StreamCompleted:
		return fmt.Sprintf("duration=%s cache_read=%s", e.Duration, formatTokenK(e.CacheReadTokens))
	case protocol.SubAgentActivityEvent:
		return compactJoin(e.Activity, e.ToolCall, e.LastAssistantMsg)
	case protocol.SubAgentFailedEvent:
		return e.Error
	case protocol.RunFailed:
		return e.Err
	default:
		return ""
	}
}

func marshalDetail(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func compactJoin(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			clean = append(clean, p)
		}
	}
	return strings.Join(clean, " ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func edgeKey(e protocol.ObservatoryEdge) string { return e.From + "->" + e.To + ":" + e.Label }

func phaseOrder(id string) int {
	order := map[string]int{"user_input": 0, "prompt_build": 1, "model_request": 2, "model_stream": 3, "tool_exec": 4, "subagents": 5, "done": 6}
	if n, ok := order[id]; ok {
		return n
	}
	return 100
}

func toolNodeID(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	return "tool:" + name
}

func agentLabel(id, description string) string {
	if description == "" {
		return id
	}
	return id + " " + description
}

func agentStatus(status string) string {
	switch status {
	case "waiting_input", "waiting_confirm", "waiting_plan":
		return "waiting"
	case "completed":
		return "done"
	case "failed", "cancelled":
		return "failed"
	case "":
		return "active"
	default:
		return "active"
	}
}

func formatTokenK(n int) string {
	if n <= 0 {
		return "0K"
	}
	return fmt.Sprintf("%dK", (n+999)/1000)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func cloneNode(n protocol.ObservatoryNode) protocol.ObservatoryNode {
	if n.Meta != nil {
		meta := make(map[string]string, len(n.Meta))
		for k, v := range n.Meta {
			meta[k] = v
		}
		n.Meta = meta
	}
	return n
}

func cloneSnapshot(s protocol.ObservatorySnapshotEvent) protocol.ObservatorySnapshotEvent {
	s.Nodes = append([]protocol.ObservatoryNode(nil), s.Nodes...)
	for i := range s.Nodes {
		s.Nodes[i] = cloneNode(s.Nodes[i])
	}
	s.Edges = append([]protocol.ObservatoryEdge(nil), s.Edges...)
	s.Phases = append([]protocol.ObservatoryPhase(nil), s.Phases...)
	s.Metrics = append([]protocol.ObservatoryMetric(nil), s.Metrics...)
	s.Evidence = append([]string(nil), s.Evidence...)
	return s
}
