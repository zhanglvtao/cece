package engine

import (
	"bytes"
	"context"
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
	session.Store
	path    string
	content []byte
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
	if err := parent.Input(context.Background(), "next step"); err != nil {
		t.Fatalf("parent input error = %v", err)
	}
	waitForTurnCompleted(t, parent)
	if len(client.messages) == 0 {
		t.Fatal("no model request captured")
	}
	last := client.messages[len(client.messages)-1]
	found := false
	for _, msg := range last {
		if strings.Contains(msg.Content, "Agent notifications from background workers") && strings.Contains(msg.Content, "Result artifact:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("request messages missing agent notification: %+v", last)
	}
}

func TestOrchestratorCompletedWritesParentInbox(t *testing.T) {
	parent := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	rt := NewAgentRuntime("agent-1", "A", "worker-model", "parent-session", NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir()), nil, context.Background(), func() {}, 8)
	rt.SessionID = "session-1"
	rt.Result = tool.AgentSubAgentResult{AgentID: "agent-1", SessionID: "session-1", Status: string(AgentStatusCompleted), Content: "done", ResultPath: "/tmp/result.txt"}
	rt.finalResult = "done"
	orch := NewOrchestrator(&fakeRuntimeFactory{rt: rt}, nil, func(protocol.Event) {})

	orch.handleTerminalMessage(parent, rt, "parent-session", AgentMessage{AgentID: "agent-1", Kind: AgentMessageResult, Status: AgentStatusCompleted, Payload: rt.Result})
	notifications := parent.drainAgentNotifications()
	if len(notifications) != 1 {
		t.Fatalf("notifications len = %d, want 1", len(notifications))
	}
	if notifications[0].AgentID != "agent-1" || notifications[0].Status != AgentStatusCompleted {
		t.Fatalf("notification = %+v", notifications[0])
	}
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
