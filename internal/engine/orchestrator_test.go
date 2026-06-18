package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/tool"
)

type fakeRuntimeFactory struct {
	rt  *AgentRuntime
	err error
}

func (f *fakeRuntimeFactory) NewSubAgentRuntime(context.Context, SubAgentBuildConfig) (*AgentRuntime, error) {
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
