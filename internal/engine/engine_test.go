package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func (f *fakeClient) SetReasoningEffort(_ string) {}

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

	waitForTurnCompleted(t, eng)
	if eng.QueuedInputCount() != 0 {
		t.Fatalf("QueuedInputCount = %d, want 0", eng.QueuedInputCount())
	}
	history := eng.History()
	if len(history) == 0 || history[0].Content != "test" {
		t.Fatalf("history = %+v, want queue input recorded as a turn", history)
	}
}

func TestEngineQueueInputStartsTurnImmediatelyWhenIdle(t *testing.T) {
	client := &recordingClient{}
	eng := NewEngine(client, tool.NewRegistry(), false, 16384, nil, "/tmp")

	eng.Do(protocol.QueueInputAction{Text: "next"})
	waitForTurnCompleted(t, eng)

	if got := eng.QueuedInputCount(); got != 0 {
		t.Fatalf("QueuedInputCount = %d, want 0 when idle queue input starts immediately", got)
	}
	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
	history := eng.History()
	if len(history) == 0 || history[0].Content != "next" {
		t.Fatalf("history = %+v, want queued input recorded as user turn", history)
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

func TestCompactPruneUsesSafeUserBoundary(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.LoadHistory(context.Background(), "", toolBoundaryHistory())

	eng.compactPrune(1)
	snapshot := eng.HistorySnapshot()
	if len(snapshot) < 2 || !snapshot[0].CompactBoundary {
		t.Fatalf("snapshot = %+v, want compact boundary", snapshot)
	}
	if !agent.IsPlainUserMessage(snapshot[1]) || snapshot[1].Content != "u0" {
		t.Fatalf("first kept message = %+v, want plain user u0", snapshot[1])
	}
}

func TestCompactSummaryUsesSafeUserBoundary(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.LoadHistory(context.Background(), "", []agent.Message{
		{Role: agent.UserRole, Content: "u0"},
		{Role: agent.AssistantRole, Content: "a0"},
		{Role: agent.UserRole, Content: "u1"},
		{Role: agent.AssistantRole, ContentBlocks: []agent.ApiContentBlock{{Type: agent.ApiToolUseContentType, ToolUse: &agent.ApiToolUseBlock{ID: "call_1", Name: "Read", Input: json.RawMessage(`{}`)}}}},
		{Role: agent.UserRole, ContentBlocks: []agent.ApiContentBlock{{Type: agent.ApiToolResultContentType, ToolResult: &agent.ApiToolResultBlock{ToolUseID: "call_1", Content: "ok"}}}},
		{Role: agent.UserRole, Content: "u2"},
	})

	_, _, _, err := eng.compactSummary(context.Background(), 2)
	if err != nil {
		t.Fatalf("compactSummary error = %v", err)
	}
	snapshot := eng.HistorySnapshot()
	if len(snapshot) < 2 || !snapshot[0].CompactBoundary {
		t.Fatalf("snapshot = %+v, want compact boundary", snapshot)
	}
	if !agent.IsPlainUserMessage(snapshot[1]) || snapshot[1].Content != "u1" {
		t.Fatalf("first kept message = %+v, want plain user u1", snapshot[1])
	}
}

func TestCompactHistoryFailureEmitsError(t *testing.T) {
	eng := NewEngine(&fakeClient{chunks: []agent.ApiStreamEvent{{Err: errors.New("summary boom")}}}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.LoadHistory(context.Background(), "", []agent.Message{
		{Role: agent.UserRole, Content: "u1"},
		{Role: agent.AssistantRole, Content: "a1"},
		{Role: agent.UserRole, Content: "u2"},
		{Role: agent.AssistantRole, Content: "a2"},
		{Role: agent.UserRole, Content: "u3"},
		{Role: agent.AssistantRole, Content: "a3"},
	})

	eng.CompactHistory(context.Background())
	for i := 0; i < 4; i++ {
		ev := <-eng.Events()
		if compacted, ok := ev.(protocol.CompactedEvent); ok {
			if !strings.Contains(compacted.Err, "summary boom") {
				t.Fatalf("CompactedEvent.Err = %q, want summary boom", compacted.Err)
			}
			return
		}
	}
	t.Fatal("expected CompactedEvent")
}

func TestCompactTrimToolResultsUsesSafeUserRange(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.LoadHistory(context.Background(), "", toolBoundaryHistory())

	trimmed, _, _ := eng.compactTrimToolResults(1, 2)
	if trimmed != 1 {
		t.Fatalf("trimmed = %d, want 1", trimmed)
	}
	history := eng.HistorySnapshot()
	tr, ok := history[2].ContentBlocks[0].AsToolResult()
	if !ok || tr.Content != "[trimmed]" {
		t.Fatalf("tool result = %+v, want trimmed", history[2].ContentBlocks)
	}
}

func toolBoundaryHistory() []agent.Message {
	return []agent.Message{
		{Role: agent.UserRole, Content: "u0"},
		{Role: agent.AssistantRole, ContentBlocks: []agent.ApiContentBlock{{Type: agent.ApiToolUseContentType, ToolUse: &agent.ApiToolUseBlock{ID: "call_1", Name: "Read", Input: json.RawMessage(`{}`)}}}},
		{Role: agent.UserRole, ContentBlocks: []agent.ApiContentBlock{{Type: agent.ApiToolResultContentType, ToolResult: &agent.ApiToolResultBlock{ToolUseID: "call_1", Content: "ok"}}}},
		{Role: agent.UserRole, Content: "u1"},
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

func (r *recordingClient) SetReasoningEffort(_ string) {}

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

func (b *blockingClient) SetReasoningEffort(_ string) {}

func TestRunSubAgentEmitsUniqueIDsForParallelAgents(t *testing.T) {
	eng := NewEngine(&recordingClient{}, tool.NewRegistry(), true, 1024, nil, t.TempDir())
	orch := NewOrchestrator(runtimeFactoryFunc(func(ctx context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error) {
		worker := NewEngine(&recordingClient{}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
		return NewAgentRuntime(cfg.AgentID, cfg.Description, "model", cfg.ParentSessionID, worker, nil, ctx, func() {}, cfg.MaxTurns), nil
	}), nil, eng.EmitEvent)
	eng.SetAgentController(orch)

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
	orch := NewOrchestrator(runtimeFactoryFunc(func(ctx context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error) {
		worker := NewEngine(&blockingClient{unblock: make(chan struct{})}, tool.NewRegistry(), false, 1024, nil, t.TempDir())
		return NewAgentRuntime(cfg.AgentID, cfg.Description, "model", cfg.ParentSessionID, worker, nil, ctx, func() {}, cfg.MaxTurns), nil
	}), nil, eng.EmitEvent)
	eng.SetAgentController(orch)
	ctx, cancel := context.WithCancel(context.Background())

	result, err := eng.AgentHandler().RunSubAgent(ctx, tool.AgentSubAgentConfig{Prompt: "a", Description: "A"}, nil)
	if err != nil {
		t.Fatalf("RunSubAgent error = %v", err)
	}
	if result.Status != string(AgentStatusStarting) && result.Status != string(AgentStatusRunning) {
		t.Fatalf("start result status = %q, want starting/running", result.Status)
	}

	// Give the sub-agent time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context
	cancel()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-eng.Events():
			failed, ok := ev.(protocol.SubAgentFailedEvent)
			if ok && failed.Error == "cancelled" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for cancelled SubAgentFailedEvent")
		}
	}
}

func TestSubAgentActivityTextUsesToolInputForToolStart(t *testing.T) {
	// subAgentActivityText was removed when we migrated to AgentRuntime bridge.
	// Equivalent logic now lives in AgentRuntime.handleEvent which tracks
	// LastTool/LastActivity directly from protocol events.
	t.Skip("migrated to AgentRuntime.handleEvent")
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

func TestRecordUsageWritesKabooLedger(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOTMUX_USAGE_DIR", dir)
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/repo")

	eng.RecordUsage(context.Background(), agent.UsageRecord{
		SessionID:                "session-1",
		Model:                    "glm-5.1",
		InputTokens:              10,
		OutputTokens:             20,
		CacheReadTokens:          3,
		CacheCreationTokens:      4,
		TotalInputTokens:         100,
		TotalOutputTokens:        200,
		TotalCacheReadTokens:     30,
		TotalCacheCreationTokens: 40,
	})

	matches, err := filepath.Glob(filepath.Join(dir, "usage-*.jsonl"))
	if err != nil {
		t.Fatalf("Glob error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("ledger files = %v, want one", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	var got struct {
		CLIID                  string `json:"cliId"`
		SessionID              string `json:"sessionId"`
		WorkingDir             string `json:"workingDir"`
		InputTokens            int    `json:"inputTokens"`
		OutputTokens           int    `json:"outputTokens"`
		CacheReadTokens        int    `json:"cacheReadTokens"`
		CacheCreateTokens      int    `json:"cacheCreateTokens"`
		TotalCacheCreateTokens int    `json:"totalCacheCreateTokens"`
	}
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got.CLIID != "claude-code" || got.SessionID != "session-1" || got.WorkingDir != "/repo" {
		t.Fatalf("record meta = %#v", got)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 || got.CacheReadTokens != 3 || got.CacheCreateTokens != 4 || got.TotalCacheCreateTokens != 40 {
		t.Fatalf("record tokens = %#v", got)
	}
}
