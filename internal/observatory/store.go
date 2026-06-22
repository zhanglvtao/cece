package observatory

import (
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
	Time time.Time `json:"time"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
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
	s.addEvidenceLocked(now, eventKind(ev), eventSummary(ev))

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
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "runtime", To: "hub", Label: "events", Status: "active"})
	s.ensureEdgeLocked(protocol.ObservatoryEdge{From: "hub", To: "engine", Label: "all events", Status: "active"})
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

func (s *Store) addEvidenceLocked(t time.Time, kind, text string) {
	if text == "" {
		text = kind
	}
	s.evidence = append(s.evidence, EvidenceItem{Time: t, Kind: kind, Text: truncate(text, 120)})
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

func eventSummary(ev protocol.Event) string {
	switch e := ev.(type) {
	case protocol.ObservatoryServerStartedEvent:
		return "observatory server " + e.URL
	case protocol.ObservatorySnapshotEvent:
		return "snapshot " + e.Scope
	case protocol.ModelRequestStarted:
		return "model request " + e.Reason
	case protocol.StreamStarted:
		return "stream started " + e.Model
	case protocol.StreamCompleted:
		if len(e.ToolCalls) > 0 {
			return "stream completed tool_use " + strings.Join(e.ToolCalls, ",")
		}
		return "stream completed " + e.StopReason
	case protocol.ToolExecStarted:
		return "tool started " + e.Name
	case protocol.ToolExecCompleted:
		return "tool completed " + e.Name
	case protocol.SubAgentStartedEvent:
		return "subagent started " + e.ID
	case protocol.SubAgentActivityEvent:
		return "subagent activity " + e.ID + " " + e.Status
	case protocol.SubAgentCompletedEvent:
		return "subagent completed " + e.ID
	case protocol.SubAgentFailedEvent:
		return "subagent failed " + e.ID
	case protocol.RunFailed:
		return "run failed"
	default:
		return eventKind(ev)
	}
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
