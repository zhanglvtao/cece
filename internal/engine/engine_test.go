package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/tool"
)

type fakeClient struct {
	chunks    []agent.ApiStreamEvent
	maxTokens int
}

func (f *fakeClient) Stream(_ context.Context, _ []agent.Message, _ agent.SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	f.maxTokens = maxTokens
	out := make(chan agent.ApiStreamEvent, len(f.chunks))
	for _, chunk := range f.chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func TestNewEngineCreatesWithDefaults(t *testing.T) {
	registry := tool.NewRegistry()
	assembler := prompt.NewContextAssembler("test", registry, nil)
	eng := NewEngine(&fakeClient{}, registry, false, 16384, assembler, "/tmp")

	if eng == nil {
		t.Fatal("NewEngine returned nil")
	}
	if eng.MaxTokens() != 16384 {
		t.Fatalf("MaxTokens = %d, want 16384", eng.MaxTokens())
	}
	if eng.Yolo() != false {
		t.Fatal("Yolo should default to false")
	}
}

func TestEngineModeCycles(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	if eng.Mode() != protocol.PermissionModeDefault {
		t.Fatalf("initial mode = %q, want default", eng.Mode())
	}
}

func TestEngineSetPermissionModeAction(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionModeAutoAccept})
	if eng.Mode() != protocol.PermissionModeAutoAccept {
		t.Fatalf("mode = %q, want auto-accept", eng.Mode())
	}
}

func TestEngineDoDispatchesActions(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	// ConfirmAction should not panic
	eng.Do(protocol.ConfirmAction{})
	eng.Do(protocol.CancelAction{})
	eng.Do(protocol.ClearHistoryAction{})
	eng.Do(protocol.QueueInputAction{Text: "test"})

	// B-class actions should be no-ops on bare Engine (handled by mediator)
	eng.Do(protocol.SwitchModelAction{Model: "test"})
	eng.Do(protocol.ListModelsAction{})
	eng.Do(protocol.CyclePermissionModeAction{})

	if eng.QueuedInputCount() != 1 {
		t.Fatalf("QueuedInputCount = %d, want 1", eng.QueuedInputCount())
	}
	eng.ClearQueuedInputs()
	if eng.QueuedInputCount() != 0 {
		t.Fatal("ClearQueuedInputs did not clear")
	}
}

func TestEngineInputValidation(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	err := eng.Input(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	err = eng.Input(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only input")
	}
}

func TestEngineEventsChannel(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	ch := eng.Events()
	if ch == nil {
		t.Fatal("Events() returned nil channel")
	}

	// EmitEvent should not block
	eng.EmitEvent(protocol.HistoryClearedEvent{})
}

func TestEngineClearHistory(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.ClearHistory()

	history := eng.History()
	if len(history) != 0 {
		t.Fatalf("History after clear = %d, want 0", len(history))
	}
}

func TestEngineHistoryRoundTrip(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	// Load some history
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "hello"},
		{Role: agent.AssistantRole, Content: "hi there"},
	}
	eng.LoadHistory(context.Background(), "test-session", msgs)

	if eng.SessionID() != "test-session" {
		t.Fatalf("SessionID = %q, want test-session", eng.SessionID())
	}
	history := eng.History()
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Role != string(agent.UserRole) {
		t.Fatalf("history[0].Role = %q, want user", history[0].Role)
	}
}

func TestEngineTurnEngineInterface(t *testing.T) {
	// Verify Engine satisfies agent.TurnEngine at compile time
	var _ agent.TurnEngine = (*Engine)(nil)

	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	if eng.ProjectDir() != "/tmp" {
		t.Fatalf("ProjectDir = %q, want /tmp", eng.ProjectDir())
	}
	if eng.HistoryLen() != 0 {
		t.Fatalf("HistoryLen = %d, want 0", eng.HistoryLen())
	}
}

func TestEngineHistorySnapshotReturnsSafeRequestHistory(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.LoadHistory(context.Background(), "test-session", []agent.Message{
		{Role: agent.UserRole, Content: "old user"},
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "old_orphan",
						Name:  "Edit",
						Input: json.RawMessage(`{"input":"bad"}`),
					},
				},
			},
		},
		{Role: agent.UserRole, Content: "summary", CompactBoundary: true},
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "kept_orphan",
						Name:  "ExitPlanMode",
						Input: json.RawMessage(`{"plan_file":"/tmp/plan.md"}`),
					},
				},
			},
		},
	})

	snapshot := eng.HistorySnapshot()
	if len(snapshot) != 3 {
		t.Fatalf("snapshot len = %d, want compact boundary + assistant + synthetic result", len(snapshot))
	}
	if snapshot[0].Content != "summary" || !snapshot[0].CompactBoundary {
		t.Fatalf("first snapshot message = %+v, want compact boundary summary", snapshot[0])
	}
	if snapshot[1].Role != agent.AssistantRole {
		t.Fatalf("second snapshot role = %q, want assistant", snapshot[1].Role)
	}
	if len(snapshot[1].ContentBlocks) != 1 || snapshot[1].ContentBlocks[0].ToolUse.ID != "kept_orphan" {
		t.Fatalf("assistant tool use = %+v, want kept_orphan only", snapshot[1].ContentBlocks)
	}
	tr, ok := snapshot[2].ContentBlocks[0].AsToolResult()
	if !ok {
		t.Fatalf("third snapshot message = %+v, want synthetic tool_result", snapshot[2])
	}
	if tr.ToolUseID != "kept_orphan" || !tr.IsError {
		t.Fatalf("synthetic result = %+v, want error result for kept_orphan", tr)
	}
}

// ── tool stubs for tool execution tests ────────────────────────────────────

type stubTool struct{}

func (stubTool) Info() tool.Definition {
	return tool.Definition{Name: "Stub", Description: "stub", InputSchema: map[string]any{"type": "object"}}
}
func (stubTool) Run(_ context.Context, _ json.RawMessage, _ tool.Emitter) tool.Result {
	return tool.Result{Content: "ok"}
}

func TestEngineDoAnswerQuestion(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	answers := []protocol.QuestionAnswer{
		{Question: "q1", Selected: []string{"a"}},
	}
	// Should not panic
	eng.Do(protocol.AnswerQuestionAction{Answers: answers})
}

// Ensure chat types available
var _ = errors.New

func TestEngineDryRunDoesNotCallModelOrMutateHistory(t *testing.T) {
	client := &fakeClient{}
	registry := tool.NewRegistry(stubTool{})
	assembler := prompt.NewContextAssembler("stable prompt", nil, nil)
	eng := NewEngine(client, registry, false, 16384, assembler, "/tmp")
	eng.AppendHistory(agent.Message{Role: agent.UserRole, Content: "old"})

	eng.Do(protocol.DryRunRequestAction{Input: "preview this"})

	if client.maxTokens != 0 {
		t.Fatalf("model was called with maxTokens=%d", client.maxTokens)
	}
	if got := eng.HistoryLen(); got != 1 {
		t.Fatalf("history len = %d, want 1", got)
	}
	select {
	case ev := <-eng.Events():
		dry, ok := ev.(protocol.RequestDryRunEvent)
		if !ok {
			t.Fatalf("event = %T, want RequestDryRunEvent", ev)
		}
		if dry.Input != "preview this" || dry.MaxTokens != 16384 {
			t.Fatalf("dryrun = %#v", dry)
		}
		if len(dry.Messages) != 2 || dry.Messages[1].Content != "preview this" {
			t.Fatalf("messages = %#v", dry.Messages)
		}
		if len(dry.Tools) != 1 || dry.Tools[0].Name != "Stub" {
			t.Fatalf("tools = %#v", dry.Tools)
		}
	default:
		t.Fatal("expected dryrun event")
	}
}

// recordingClient records all Stream calls for verification.
type recordingClient struct {
	calls    int
	messages [][]agent.Message
}

func (r *recordingClient) Stream(_ context.Context, messages []agent.Message, _ agent.SystemPrompt, _ []tool.Definition, _ int) (<-chan agent.ApiStreamEvent, error) {
	r.calls++
	cp := make([]agent.Message, len(messages))
	copy(cp, messages)
	r.messages = append(r.messages, cp)

	text := "assistant response"
	if r.calls == 1 && len(messages) > 2 {
		text = "compact summary"
	}
	out := make(chan agent.ApiStreamEvent, 5)
	out <- agent.ApiStreamEvent{EventType: "message_start", InputTokens: 10}
	out <- agent.ApiStreamEvent{EventType: "content_block_start", Index: 0, Detail: "text"}
	out <- agent.ApiStreamEvent{Delta: text, Detail: "text_delta"}
	out <- agent.ApiStreamEvent{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 5}
	out <- agent.ApiStreamEvent{Done: true, EventType: "message_stop"}
	close(out)
	return out, nil
}

func waitForTurnCompleted(t *testing.T, eng *Engine) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-eng.Events():
			if _, ok := ev.(protocol.TurnCompleted); ok {
				return
			}
		case <-timeout:
			t.Fatal("expected TurnCompleted event")
		}
	}
}

// blockingClient blocks until its stream context is cancelled, then returns.
type blockingClient struct {
	unblock chan struct{} // close to unblock (optional)
}

func (b *blockingClient) Stream(ctx context.Context, _ []agent.Message, _ agent.SystemPrompt, _ []tool.Definition, _ int) (<-chan agent.ApiStreamEvent, error) {
	out := make(chan agent.ApiStreamEvent)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
		case <-b.unblock:
		}
	}()
	return out, nil
}

func TestRunSubAgentEmitsUniqueIDsForParallelAgents(t *testing.T) {
	eng := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())

	ctx := context.Background()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = eng.AgentHandler().RunSubAgent(ctx, tool.AgentSubAgentConfig{Prompt: "a", Description: "A"}, nil)
		done <- struct{}{}
	}()
	go func() {
		_, _ = eng.AgentHandler().RunSubAgent(ctx, tool.AgentSubAgentConfig{Prompt: "b", Description: "B"}, nil)
		done <- struct{}{}
	}()

	<-done
	<-done

	ids := map[string]bool{}
	for len(ids) < 2 {
		select {
		case ev := <-eng.Events():
			if started, ok := ev.(protocol.SubAgentStartedEvent); ok {
				ids[started.ID] = true
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for two SubAgentStartedEvent; ids=%v", ids)
		}
	}
}

func TestRunSubAgentEmitsFailedOnCancellation(t *testing.T) {
	eng := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = eng.AgentHandler().RunSubAgent(ctx, tool.AgentSubAgentConfig{Prompt: "a", Description: "A"}, nil)

	for {
		select {
		case ev := <-eng.Events():
			if failed, ok := ev.(protocol.SubAgentFailedEvent); ok {
				if failed.Description != "A" || failed.Error == "" {
					t.Fatalf("failed event = %#v", failed)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for SubAgentFailedEvent")
		}
	}
}

func TestSubAgentActivityTextUsesToolInputForToolStart(t *testing.T) {
	labels := map[string]string{}
	activity := subAgentActivityText(agent.ToolCallCompleted{
		ID:    "tool-1",
		Name:  "Read",
		Input: json.RawMessage(`{"path":"/tmp/file.go"}`),
	}, labels)
	if !strings.Contains(activity, "/tmp/file.go") {
		t.Fatalf("activity = %q, want path", activity)
	}
	activity = subAgentActivityText(agent.ToolExecStarted{ID: "tool-1", Name: "Read"}, labels)
	if !strings.Contains(activity, "/tmp/file.go") {
		t.Fatalf("exec activity = %q, want cached path", activity)
	}
}

func TestEngineInterruptPreservesHistory(t *testing.T) {
	client := &blockingClient{unblock: make(chan struct{})}
	eng := NewEngine(client, tool.NewRegistry(), false, 16384, nil, "/tmp")

	// Pre-populate some history
	eng.AppendHistory(agent.Message{Role: agent.UserRole, Content: "previous question"})
	eng.AppendHistory(agent.Message{Role: agent.AssistantRole, Content: "previous answer"})
	if eng.HistoryLen() != 2 {
		t.Fatalf("pre-condition: HistoryLen = %d, want 2", eng.HistoryLen())
	}

	// Start a turn
	ctx := context.Background()
	if err := eng.Input(ctx, "hello"); err != nil {
		t.Fatalf("Input error: %v", err)
	}

	// Give the goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the turn
	eng.Cancel()

	// Wait for turn to complete
	waitForTurnCompleted(t, eng)

	// History should be preserved — the "hello" user message and interrupt
	// marker should remain (no rollback).
	if eng.HistoryLen() < 3 {
		history := eng.History()
		t.Fatalf("HistoryLen after cancel = %d, want >= 3; history = %+v", eng.HistoryLen(), history)
	}
}

func TestBuildContextNudgeReminderFramesAsContextManagement(t *testing.T) {
	reminder := buildContextNudgeReminder(80, 8, 10, 3)

	checks := []string{
		"Context pressure: 80% used (8K/10K), 3 turns since last context management.",
		"Manage context as needed.",
		"Compact, TrimToolResults, or Prune",
		"based on what best fits the current state",
	}
	for _, check := range checks {
		if !strings.Contains(reminder, check) {
			t.Fatalf("reminder missing %q: %s", check, reminder)
		}
	}
}
