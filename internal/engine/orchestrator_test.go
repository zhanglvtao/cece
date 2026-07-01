package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type fakeRuntimeFactory struct {
	rt      *AgentRuntime
	err     error
	lastCfg SubAgentBuildConfig
}

type artifactStore struct {
	path     string
	content  []byte
	sessions map[string]session.Session
}

func (s *artifactStore) Create(_ context.Context, title string) (*session.Session, error) {
	if s.sessions == nil {
		s.sessions = make(map[string]session.Session)
	}
	id := "session-1"
	sess := session.Session{ID: id, Title: title, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	s.sessions[id] = sess
	return &sess, nil
}

func (s *artifactStore) AppendMessage(_ context.Context, _ string, _ json.RawMessage) error {
	return nil
}

func (s *artifactStore) LoadMessages(_ context.Context, _ string) ([]json.RawMessage, error) {
	return nil, nil
}

func (s *artifactStore) List(_ context.Context) ([]session.Session, error) {
	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out, nil
}

func (s *artifactStore) Get(_ context.Context, id string) (*session.Session, error) {
	if sess, ok := s.sessions[id]; ok {
		return &sess, nil
	}
	return nil, nil
}

func (s *artifactStore) Rename(_ context.Context, id, title string) error {
	if s.sessions == nil {
		s.sessions = make(map[string]session.Session)
	}
	sess := s.sessions[id]
	sess.ID = id
	sess.Title = title
	sess.UpdatedAt = time.Now()
	s.sessions[id] = sess
	return nil
}

func (s *artifactStore) Delete(_ context.Context, id string) error {
	delete(s.sessions, id)
	return nil
}

func (s *artifactStore) UpdateMeta(_ context.Context, _ string, _ session.SessionMeta) error {
	return nil
}

func (s *artifactStore) SaveInputHistory(_ context.Context, _ string, _ []string) error {
	return nil
}

func (s *artifactStore) WriteArtifact(_ context.Context, sessionID, name string, content []byte) (string, error) {
	s.content = append([]byte(nil), content...)
	if s.path == "" {
		s.path = "/tmp/cece-test-artifact/" + sessionID + "/" + name
	}
	return s.path, nil
}

func (s *artifactStore) ArtifactPath(_ context.Context, sessionID, name string) (string, error) {
	if s.path == "" {
		return "/tmp/cece-test-artifact/" + sessionID + "/" + name, nil
	}
	return s.path, nil
}

func (f *fakeRuntimeFactory) NewSubAgentRuntime(_ context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error) {
	f.lastCfg = cfg
	return f.rt, f.err
}

type runtimeFactoryFunc func(context.Context, SubAgentBuildConfig) (*AgentRuntime, error)

func (f runtimeFactoryFunc) NewSubAgentRuntime(ctx context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error) {
	return f(ctx, cfg)
}

func TestEngineAgentHandlerDelegatesToAgentController(t *testing.T) {
	eng := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	eng.SetAgentController(agentControllerFunc(func(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
		return tool.AgentSubAgentResult{AgentID: "agent-1", Status: string(AgentStatusRunning), Content: "started"}, nil
	}))

	result, err := eng.AgentHandler().RunSubAgent(context.Background(), tool.AgentSubAgentConfig{Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("RunSubAgent error = %v", err)
	}
	if result.AgentID != "agent-1" {
		t.Fatalf("AgentID = %q, want agent-1", result.AgentID)
	}
	if result.Status != string(AgentStatusRunning) {
		t.Fatalf("Status = %q, want %q", result.Status, AgentStatusRunning)
	}
}

func TestOrchestratorStartReturnsImmediately(t *testing.T) {
	block := make(chan struct{})
	workerEngine := NewEngine(&blockingClient{unblock: block}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	start := time.Now()
	result, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("start should return immediately without waiting for worker completion")
	}
	if result.Status != string(AgentStatusRunning) && result.Status != string(AgentStatusStarting) {
		t.Fatalf("Status = %q, want starting/running", result.Status)
	}
	if !strings.Contains(result.Content, "spawning agent's inbox") {
		t.Fatalf("start content = %q, want spawning inbox guidance", result.Content)
	}
	if strings.Contains(result.Content, "status or wait") || strings.Contains(result.Content, "check sooner") {
		t.Fatalf("start content encourages polling: %q", result.Content)
	}
	close(block)
}

func TestOrchestratorStartUsesParentModelWhenOmitted(t *testing.T) {
	block := make(chan struct{})
	workerEngine := NewEngine(&blockingClient{unblock: block}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "parent-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	var capturedModel string
	factory := runtimeFactoryFunc(func(ctx context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error) {
		capturedModel = cfg.Model
		return rt, nil
	})
	orch := NewOrchestrator(factory, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	parent.SetModelInfo("parent-model", 123000)

	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil); err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}
	if capturedModel != "parent-model" {
		t.Fatalf("factory model = %q, want parent-model", capturedModel)
	}
	close(block)
}

func TestOrchestratorStatusAndCancel(t *testing.T) {
	workerEngine := NewEngine(&blockingClient{unblock: make(chan struct{})}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	_, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	status, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "status", AgentID: "agent-1"}, nil)
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if status.AgentID != "agent-1" {
		t.Fatalf("status.AgentID = %q, want agent-1", status.AgentID)
	}
	cancelled, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "cancel", AgentID: "agent-1"}, nil)
	if err != nil {
		t.Fatalf("cancel error = %v", err)
	}
	if !cancelled.Cancelled {
		t.Fatal("cancel should mark result as cancelled")
	}
}

func TestOrchestratorWaitTimeoutAndCompletedResult(t *testing.T) {
	workerEngine := NewEngine(&blockingClient{unblock: make(chan struct{})}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", workerEngine, nil, context.Background(), func() {}, 8)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil); err != nil {
		t.Fatalf("start error = %v", err)
	}
	timedOut, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "wait", AgentID: "agent-1", TimeoutMS: 1}, nil)
	if err != nil {
		t.Fatalf("wait timeout error = %v", err)
	}
	if timedOut.Err != "" || !strings.Contains(timedOut.Content, "timed out") {
		t.Fatalf("wait timeout result = %+v, want non-error timeout", timedOut)
	}

	rt.Result = tool.AgentSubAgentResult{AgentID: "agent-1", SessionID: "session-1", Status: string(AgentStatusCompleted), Content: "done", ResultPath: "/tmp/result.txt"}
	rt.record(AgentMessage{AgentID: "agent-1", Kind: AgentMessageResult, Status: AgentStatusCompleted, Payload: rt.Result})
	completed, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "wait", AgentID: "agent-1", TimeoutMS: 1000}, nil)
	if err != nil {
		t.Fatalf("wait completed error = %v", err)
	}
	if completed.Status != string(AgentStatusCompleted) || completed.Content != "done" || completed.ResultPath != "/tmp/result.txt" {
		t.Fatalf("completed = %+v, want completed result with artifact", completed)
	}
}

func TestOrchestratorCompletedBackfillsArtifact(t *testing.T) {
	store := &artifactStore{}
	worker := NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", worker, nil, context.Background(), func() {}, 8)
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	parent.SetStore(store)
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, store, func(protocol.Event) {})

	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil); err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		status, _ := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "status", AgentID: "agent-1"}, nil)
		if status.Status == string(AgentStatusCompleted) && status.ResultPath != "" {
			if !strings.Contains(status.ResultPath, "result.txt") {
				t.Fatalf("ResultPath = %q, want result.txt", status.ResultPath)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for completed artifact; store content=%q", string(store.content))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	client := &recordingClient{}
	parent = NewEngine(client, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	parent.appendAgentNotification(agentNotification{AgentID: "agent-1", Status: AgentStatusCompleted, Summary: "assistant response", ResultPath: store.path})
	waitForTurnCompleted(t, parent)
	if len(client.messages) == 0 {
		t.Fatal("no model request captured")
	}
	found := false
	for _, request := range client.messages {
		for _, msg := range request {
			if strings.Contains(msg.Content, "Agent notifications from spawned agents") && strings.Contains(msg.Content, "Result artifact:") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("request messages missing agent notification: %+v", client.messages)
	}

	if err := parent.Input(context.Background(), "next step"); err != nil {
		t.Fatalf("parent input error = %v", err)
	}
	waitForTurnCompleted(t, parent)
	last := client.messages[len(client.messages)-1]
	for _, msg := range last {
		if strings.Contains(msg.Content, "Agent notifications from spawned agents") {
			t.Fatalf("notification injected twice: %+v", last)
		}
	}
}

func TestOrchestratorCompletedWritesParentInbox(t *testing.T) {
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir()), nil, context.Background(), func() {}, 8)
	rt.SessionID = "session-1"
	rt.Result = tool.AgentSubAgentResult{AgentID: "agent-1", SessionID: "session-1", Status: string(AgentStatusCompleted), Content: "done", ResultPath: "/tmp/result.txt"}
	rt.finalResult = "done"
	var events []protocol.Event
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(ev protocol.Event) { events = append(events, ev) })

	orch.handleTerminalMessage(parent, rt, "parent-session", AgentMessage{AgentID: "agent-1", Kind: AgentMessageResult, Status: AgentStatusCompleted, Payload: rt.Result})
	notifications := parent.drainAgentNotifications()
	if len(notifications) != 1 {
		t.Fatalf("notifications len = %d, want 1", len(notifications))
	}
	if notifications[0].AgentID != "agent-1" || notifications[0].Status != AgentStatusCompleted {
		t.Fatalf("notification = %+v", notifications[0])
	}
	bus, ok := findAgentBusEvent(events, "interactive-root", "inbox", "result")
	if !ok {
		t.Fatalf("missing root inbox result AgentBusEvent: %+v", events)
	}
	if bus.TraceID != "agent-1" || bus.ParentSessionID != "parent-session" || bus.SessionID != "session-1" {
		t.Fatalf("root inbox event = %+v", bus)
	}
	if bus.Payload["result_path"] != "/tmp/result.txt" || bus.Payload["summary"] != "done" || bus.Payload["status"] != string(AgentStatusCompleted) {
		t.Fatalf("root inbox payload = %+v", bus.Payload)
	}
}

func TestOrchestratorBridgeConsumesBestEffortProgress(t *testing.T) {
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir()), nil, context.Background(), func() {}, 8)
	rt.SessionID = "session-1"
	rt.Model = "worker-model"
	rt.InputTokens = 11
	rt.OutputTokens = 7
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt.Context = ctx
	var events []protocol.Event
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(ev protocol.Event) { events = append(events, ev) })

	go orch.bridgeRuntime(parent, rt, "parent-session")
	rt.record(AgentMessage{AgentID: "agent-1", Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: map[string]any{"activity": "reading files"}})

	deadline := time.After(time.Second)
	for {
		if bus, ok := findAgentBusEvent(events, "agent-1", "outbox", "progress"); ok {
			if bus.Payload["activity"] != "reading files" {
				t.Fatalf("progress payload = %+v", bus.Payload)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("missing best-effort progress AgentBusEvent: %+v", events)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !hasSubAgentActivity(events, "agent-1", "reading files") {
		t.Fatalf("missing SubAgentActivityEvent: %+v", events)
	}
}

func TestOrchestratorPendingResponseUsesCausationID(t *testing.T) {
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir()), nil, context.Background(), func() {}, 8)
	rt.SessionID = "session-1"
	var events []protocol.Event
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(ev protocol.Event) { events = append(events, ev) })
	orch.mu.Lock()
	orch.agents[rt.ID] = rt
	orch.mu.Unlock()

	orch.handleRuntimeAgentEvent(parent, rt, "parent-session", AgentMessage{ID: "pending-1", AgentID: "agent-1", Kind: AgentMessageQuestion, Status: AgentStatusWaitingInput})
	if _, err := orch.answer(tool.AgentSubAgentConfig{AgentID: "agent-1", Answers: []tool.QuestionAnswer{{Question: "Proceed?", Selected: []string{"Yes"}}}}); err != nil {
		t.Fatalf("answer error = %v", err)
	}

	bus, ok := findAgentBusEvent(events, "agent-1", "inbox", string(AgentCommandAnswerQuestion))
	if !ok {
		t.Fatalf("missing answer inbox AgentBusEvent: %+v", events)
	}
	if bus.CausationID != "pending-1" {
		t.Fatalf("CausationID = %q, want pending-1", bus.CausationID)
	}
}

func findAgentBusEvent(events []protocol.Event, agentID, lane, kind string) (protocol.AgentBusEvent, bool) {
	for _, ev := range events {
		bus, ok := ev.(protocol.AgentBusEvent)
		if ok && bus.AgentID == agentID && bus.Lane == lane && bus.Kind == kind {
			return bus, true
		}
	}
	return protocol.AgentBusEvent{}, false
}

func hasSubAgentActivity(events []protocol.Event, agentID, activity string) bool {
	for _, ev := range events {
		activityEvent, ok := ev.(protocol.SubAgentActivityEvent)
		if ok && activityEvent.ID == agentID && activityEvent.Activity == activity {
			return true
		}
	}
	return false
}

func TestOrchestratorLogsWorkerLifecycle(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	orig := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(orig) })

	worker := NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", worker, nil, context.Background(), func() {}, 8)
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	parent.LoadHistory(context.Background(), "parent-session", nil)
	var events []protocol.Event
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(ev protocol.Event) { events = append(events, ev) })

	if _, err := orch.Run(context.Background(), parent, tool.AgentSubAgentConfig{Operation: "start", Prompt: "analyze", Description: "A"}, nil); err != nil {
		t.Fatalf("Run(start) error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		if strings.Contains(buf.String(), "orchestrator: state transition") && strings.Contains(buf.String(), "status=completed") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for orchestrator lifecycle logs:\n%s", buf.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	logs := buf.String()
	checks := []string{
		"orchestrator: worker started",
		"agent_id=agent-1",
		"profile=worker",
		"parent_session_id=parent-session",
		"orchestrator: state transition",
		"status=completed",
	}
	for _, check := range checks {
		if !strings.Contains(logs, check) {
			t.Fatalf("logs missing %q:\n%s", check, logs)
		}
	}
}
