package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type dryRunEngine struct {
	assembler *prompt.ContextAssembler
	registry  *tool.Registry
	history   []Message
	planState *tool.PlanModeState
	yolo      bool
}

func (e *dryRunEngine) ProjectDir() string                  { return "/repo" }
func (e *dryRunEngine) Assembler() *prompt.ContextAssembler { return e.assembler }
func (e *dryRunEngine) Client() ModelClient                 { return nil }
func (e *dryRunEngine) Registry() *tool.Registry            { return e.registry }
func (e *dryRunEngine) PlanState() *tool.PlanModeState {
	if e.planState == nil {
		return tool.NewPlanModeState()
	}
	return e.planState
}
func (e *dryRunEngine) TaskList() *tool.TaskList { return tool.NewTaskList() }
func (e *dryRunEngine) TaskClosureState() *tool.TaskClosureState {
	return tool.NewTaskClosureState()
}
func (e *dryRunEngine) Yolo() bool                              { return e.yolo }
func (e *dryRunEngine) MaxTokens() int                          { return 1234 }
func (e *dryRunEngine) ContextWindow() int                      { return 270000 }
func (e *dryRunEngine) ToolResultPolicy() ToolResultPolicy      { return ToolResultPolicy{} }
func (e *dryRunEngine) SessionID() string                       { return "" }
func (e *dryRunEngine) HistoryLen() int                         { return len(e.history) }
func (e *dryRunEngine) AppendHistory(msg Message)               { e.history = append(e.history, msg) }
func (e *dryRunEngine) PersistMessage(context.Context, Message) {}
func (e *dryRunEngine) HistorySnapshot() []Message {
	return append([]Message(nil), e.history...)
}
func (e *dryRunEngine) SetLastInputTokens(int) {}
func (e *dryRunEngine) IncrementTokens(int, int) (string, session.SessionMeta, bool) {
	return "", session.SessionMeta{}, false
}
func (e *dryRunEngine) RecordUsage(context.Context, UsageRecord) {}
func (e *dryRunEngine) IncrementAPICalls()                       {}
func (e *dryRunEngine) RecordToolExecution(string, bool)         {}
func (e *dryRunEngine) UpdateCacheTokens(int, int)               {}
func (e *dryRunEngine) RecordClosureEvidence(ClosureEvidence)    {}
func (e *dryRunEngine) ClosureEvidenceSnapshot() []ClosureEvidence {
	return nil
}
func (e *dryRunEngine) ResetQuestionAnswers()                     {}
func (e *dryRunEngine) GetQuestionAnswers() []tool.QuestionAnswer { return nil }
func (e *dryRunEngine) DrainQueuedInputs() []string               { return nil }
func (e *dryRunEngine) TryAutoCompact(ctx context.Context) bool   { return false }

type dryRunCollector struct{}

func (dryRunCollector) Collect(context.Context) (prompt.SessionContext, error) {
	return prompt.SessionContext{RepoRoot: "/repo", CLAUDEmd: "project rule"}, nil
}

type dryRunTool struct{}

func (dryRunTool) Info() tool.Definition {
	return tool.Definition{Name: "Read", Description: "read files", InputSchema: map[string]any{"type": "object"}}
}
func (dryRunTool) Run(context.Context, json.RawMessage, tool.Emitter) tool.Result {
	return tool.Result{}
}

func TestBuildDryRunRequestIncludesLayersMessagesAndTools(t *testing.T) {
	assembler := prompt.NewContextAssembler("stable prompt", nil, dryRunCollector{})
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	eng := &dryRunEngine{
		assembler: assembler,
		registry:  tool.NewRegistry(dryRunTool{}),
		history:   []Message{{Role: UserRole, Content: "old"}},
	}
	bootstrap := NewTurnBootstrap(eng, nil, nil, nil)
	plan := bootstrap.BuildTurnPlan("hello", []Message{{Role: UserRole, Content: "hello"}})
	dry := bootstrap.BuildDryRunRequest("hello", plan)

	if dry.Input != "hello" || dry.MaxTokens != 1234 {
		t.Fatalf("dryrun meta = %#v", dry)
	}
	if len(dry.PromptLayers) != 3 {
		t.Fatalf("layers = %d, want 3", len(dry.PromptLayers))
	}
	if dry.PromptLayers[0].Name != "stable" || dry.PromptLayers[1].Name != "session" || dry.PromptLayers[2].Name != "turn" {
		t.Fatalf("layers = %#v", dry.PromptLayers)
	}
	if len(dry.Messages) != 1 || dry.Messages[0].Content != "hello" {
		t.Fatalf("messages = %#v", dry.Messages)
	}
	if len(dry.Tools) != 1 || dry.Tools[0].Name != "Read" {
		t.Fatalf("tools = %#v", dry.Tools)
	}
	if dry.EstimatedInputTokens <= 0 {
		t.Fatalf("EstimatedInputTokens = %d, want > 0", dry.EstimatedInputTokens)
	}
}

func TestBuildTurnPlanIncludesModeAndTaskReminder(t *testing.T) {
	assembler := prompt.NewContextAssembler("stable prompt", nil, dryRunCollector{})
	if _, err := assembler.RefreshSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	planState := tool.NewPlanModeState()
	planState.SetMode(tool.PermissionModePlan)
	eng := &dryRunEngine{
		assembler: assembler,
		registry:  tool.NewRegistry(),
		planState: planState,
		yolo:      true,
	}
	bootstrap := NewTurnBootstrap(eng, nil, nil, nil)
	plan := bootstrap.BuildTurnPlan("failing test needs fix", []Message{{Role: UserRole, Content: "failing test needs fix"}})

	if len(plan.AssembleResult.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(plan.AssembleResult.Segments))
	}
	turn := plan.AssembleResult.Segments[2].Content
	for _, want := range []string{"permission_mode: plan", "yolo: true", "<task_reminder>", "Bugfix/test-failure task detected."} {
		if !strings.Contains(turn, want) {
			t.Fatalf("turn layer = %q, want %q", turn, want)
		}
	}
}

func TestTurnRunnerExitPlanModeRejectedPersistsRejectToolResult(t *testing.T) {
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(t.TempDir())
	planState.SetMode(tool.PermissionModePlan)
	planFile := filepath.Join(planState.PlansDir(), "plan.md")
	if err := os.WriteFile(planFile, []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	streamCalls := 0
	client := &mockStreamClient{streamFn: func(context.Context, []Message, SystemPrompt, []tool.Definition, int) (<-chan ApiStreamEvent, error) {
		streamCalls++
		ch := make(chan ApiStreamEvent, 8)
		ch <- ApiStreamEvent{EventType: "message_start", InputTokens: 10}
		ch <- ApiStreamEvent{EventType: "content_block_start", Index: 0, ToolCallID: "call_exit", ToolCallName: tool.ExitPlanModeToolName}
		ch <- ApiStreamEvent{EventType: "content_block_delta", Detail: "input_json_delta", Index: 0, ToolCallInput: `{"plan_file":"` + planFile + `"}`}
		ch <- ApiStreamEvent{EventType: "content_block_stop", Index: 0}
		ch <- ApiStreamEvent{EventType: "message_delta", StopReason: "tool_use", OutputTokens: 5}
		ch <- ApiStreamEvent{Done: true, EventType: "message_stop"}
		close(ch)
		return ch, nil
	}}

	var history []Message
	var persisted []Message
	rejectCh := make(chan struct{}, 1)
	events := make(chan Event, 32)
	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		NewInteractionGate(tool.NewRegistry(), planState, false, nil, rejectCh, nil),
		nil,
		4096,
		TurnDeps{
			AppendMessage:     func(m Message) { history = append(history, m) },
			PersistMessage:    func(_ context.Context, m Message) { persisted = append(persisted, m) },
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			HistorySnapshot:   func() []Message { return append([]Message(nil), history...) },
			IncrementAPICalls: func() {},
		},
	)

	done := make(chan struct{})
	go func() {
		runner.Run(context.Background(), TurnPlan{Messages: []Message{{Role: UserRole, Content: "start"}}}, events)
		close(done)
	}()

	waitForEventType(t, events, PlanApprovalRequested{})
	rejectCh <- struct{}{}
	<-done

	if streamCalls != 1 {
		t.Fatalf("streamCalls = %d, want 1", streamCalls)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want assistant + reject tool_result", len(history))
	}
	resultMsg := history[1]
	if resultMsg.Role != ToolRole || len(resultMsg.ContentBlocks) != 1 {
		t.Fatalf("reject message = %+v", resultMsg)
	}
	tr, ok := resultMsg.ContentBlocks[0].AsToolResult()
	if !ok || tr.ToolUseID != "call_exit" || !tr.IsError {
		t.Fatalf("tool result = %+v, ok=%v", resultMsg.ContentBlocks[0], ok)
	}
	if !containsMessage(persisted, resultMsg) {
		t.Fatalf("persisted messages = %+v, missing reject tool_result", persisted)
	}
	waitForEventType(t, events, PlanRejected{})
	waitForEventType(t, events, AssistantCompleted{})
}

func waitForEventType[T Event](t *testing.T, events <-chan Event, _ T) T {
	t.Helper()
	for i := 0; i < 32; i++ {
		ev := <-events
		if got, ok := ev.(T); ok {
			return got
		}
	}
	t.Fatalf("event %T not emitted", *new(T))
	var zero T
	return zero
}

func containsMessage(messages []Message, target Message) bool {
	for _, msg := range messages {
		if msg.Role == target.Role && len(msg.ContentBlocks) == len(target.ContentBlocks) {
			if len(msg.ContentBlocks) == 0 {
				return msg.Content == target.Content
			}
			got, gotOK := msg.ContentBlocks[0].AsToolResult()
			want, wantOK := target.ContentBlocks[0].AsToolResult()
			if gotOK && wantOK && got.ToolUseID == want.ToolUseID && got.Content == want.Content && got.IsError == want.IsError {
				return true
			}
		}
	}
	return false
}

func TestTurnRunnerCompletionGateBlocksAndContinues(t *testing.T) {
	responses := []<-chan ApiStreamEvent{textStream("done early"), textStream("done after reminder")}
	streamCalls := 0
	var secondRequest []Message
	client := &mockStreamClient{streamFn: func(_ context.Context, messages []Message, _ SystemPrompt, _ []tool.Definition, _ int) (<-chan ApiStreamEvent, error) {
		if streamCalls == 1 {
			secondRequest = append([]Message(nil), messages...)
		}
		resp := responses[streamCalls]
		streamCalls++
		return resp, nil
	}}

	var history []Message
	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		4096,
		TurnDeps{
			AppendMessage:     func(m Message) { history = append(history, m) },
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			HistorySnapshot:   func() []Message { return append([]Message(nil), history...) },
			IncrementAPICalls: func() {},
			CompletionGateContext: func() CompletionGateContext {
				if streamCalls >= 2 {
					return CompletionGateContext{Closure: tool.TaskClosureSnapshot{
						Updated:            true,
						NeedsCodeChange:    tool.ClosureDecisionNo,
						CodeChangeStatus:   tool.ClosureCodeNotNeeded,
						CodeChangeReason:   "blocked reminder handled in test",
						NeedsVerification:  tool.ClosureDecisionNo,
						VerificationStatus: tool.ClosureVerificationNotNeeded,
						VerificationReason: "not needed in test",
					}}
				}
				return CompletionGateContext{Closure: tool.TaskClosureSnapshot{}}
			},
		},
	)

	events := make(chan Event, 32)
	runner.Run(context.Background(), TurnPlan{Messages: []Message{{Role: UserRole, Content: "fix bug"}}}, events)

	if got := waitForEventType(t, events, CompletionGateEvaluated{}); got.Status != CompletionGateBlocked || got.Next != "continue" {
		t.Fatalf("CompletionGateEvaluated = %+v, want blocked continue", got)
	}
	if got := waitForEventType(t, events, ModelRequestStarted{}); got.Reason != "completion_gate" {
		t.Fatalf("ModelRequestStarted.Reason = %q, want completion_gate", got.Reason)
	}
	if streamCalls != 2 {
		t.Fatalf("streamCalls = %d, want 2", streamCalls)
	}
	if len(secondRequest) == 0 || !strings.Contains(secondRequest[len(secondRequest)-1].TextContent(), "Completion gate blocked") {
		t.Fatalf("secondRequest = %+v, want completion gate reminder", secondRequest)
	}
}

func TestTurnRunnerCompletionGatePassesWithClosure(t *testing.T) {
	streamCalls := 0
	client := &mockStreamClient{streamFn: func(_ context.Context, _ []Message, _ SystemPrompt, _ []tool.Definition, _ int) (<-chan ApiStreamEvent, error) {
		streamCalls++
		return textStream("done"), nil
	}}
	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		4096,
		TurnDeps{
			AppendMessage:     func(Message) {},
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			HistorySnapshot:   func() []Message { return nil },
			IncrementAPICalls: func() {},
			CompletionGateContext: func() CompletionGateContext {
				return CompletionGateContext{}
			},
		},
	)

	events := make(chan Event, 32)
	runner.Run(context.Background(), TurnPlan{Messages: []Message{{Role: UserRole, Content: "explain"}}}, events)

	if streamCalls != 1 {
		t.Fatalf("streamCalls = %d, want 1", streamCalls)
	}
	if got := waitForEventType(t, events, CompletionGateEvaluated{}); got.Status != CompletionGatePassed || got.Next != "complete" {
		t.Fatalf("CompletionGateEvaluated = %+v, want passed complete", got)
	}
	waitForEventType(t, events, AssistantCompleted{})
}

func TestTurnRunnerCompletionGateKeepsRetryingAfterThreeBlockedAttempts(t *testing.T) {
	responses := []<-chan ApiStreamEvent{
		textStream("attempt 1"),
		textStream("attempt 2"),
		textStream("attempt 3"),
		textStream("attempt 4"),
	}
	streamCalls := 0
	client := &mockStreamClient{streamFn: func(_ context.Context, _ []Message, _ SystemPrompt, _ []tool.Definition, _ int) (<-chan ApiStreamEvent, error) {
		resp := responses[streamCalls]
		streamCalls++
		return resp, nil
	}}

	var history []Message
	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		4096,
		TurnDeps{
			AppendMessage:     func(m Message) { history = append(history, m) },
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			HistorySnapshot:   func() []Message { return append([]Message(nil), history...) },
			IncrementAPICalls: func() {},
			CompletionGateContext: func() CompletionGateContext {
				if streamCalls >= 4 {
					return CompletionGateContext{Closure: tool.TaskClosureSnapshot{
						Updated:            true,
						NeedsCodeChange:    tool.ClosureDecisionNo,
						CodeChangeStatus:   tool.ClosureCodeNotNeeded,
						CodeChangeReason:   "resolved after repeated reminders",
						NeedsVerification:  tool.ClosureDecisionNo,
						VerificationStatus: tool.ClosureVerificationNotNeeded,
						VerificationReason: "not needed in test",
					}}
				}
				return CompletionGateContext{Closure: tool.TaskClosureSnapshot{}}
			},
		},
	)

	events := make(chan Event, 64)
	runner.Run(context.Background(), TurnPlan{Messages: []Message{{Role: UserRole, Content: "fix bug"}}}, events)

	blocked := 0
	for blocked < 3 {
		got := waitForEventType(t, events, CompletionGateEvaluated{})
		if got.Status != CompletionGateBlocked || got.Next != "continue" {
			t.Fatalf("CompletionGateEvaluated = %+v, want blocked continue", got)
		}
		blocked++
	}
	if got := waitForEventType(t, events, CompletionGateEvaluated{}); got.Status != CompletionGatePassed || got.Next != "complete" {
		t.Fatalf("CompletionGateEvaluated = %+v, want passed complete", got)
	}
	if streamCalls != 4 {
		t.Fatalf("streamCalls = %d, want 4", streamCalls)
	}
	waitForEventType(t, events, AssistantCompleted{})
}

func TestTurnRunnerCompletionGateEscalatesAfterNoProgress(t *testing.T) {
	responses := []<-chan ApiStreamEvent{
		textStream("attempt 1"),
		textStream("attempt 2"),
		textStream("attempt 3"),
		textStream("attempt 4"),
	}
	streamCalls := 0
	var fourthRequest []Message
	client := &mockStreamClient{streamFn: func(_ context.Context, messages []Message, _ SystemPrompt, _ []tool.Definition, _ int) (<-chan ApiStreamEvent, error) {
		if streamCalls == 3 {
			fourthRequest = append([]Message(nil), messages...)
		}
		resp := responses[streamCalls]
		streamCalls++
		return resp, nil
	}}

	var history []Message
	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		4096,
		TurnDeps{
			AppendMessage:     func(m Message) { history = append(history, m) },
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			HistorySnapshot:   func() []Message { return append([]Message(nil), history...) },
			IncrementAPICalls: func() {},
			CompletionGateContext: func() CompletionGateContext {
				if streamCalls >= 4 {
					return CompletionGateContext{Closure: tool.TaskClosureSnapshot{
						Updated:            true,
						NeedsCodeChange:    tool.ClosureDecisionNo,
						CodeChangeStatus:   tool.ClosureCodeNotNeeded,
						CodeChangeReason:   "resolved after escalation",
						NeedsVerification:  tool.ClosureDecisionNo,
						VerificationStatus: tool.ClosureVerificationNotNeeded,
						VerificationReason: "not needed in test",
					}}
				}
				return CompletionGateContext{TaskList: []tool.TodoItem{{Content: "x", Status: tool.TodoInProgress}}}
			},
		},
	)

	events := make(chan Event, 64)
	runner.Run(context.Background(), TurnPlan{Messages: []Message{{Role: UserRole, Content: "fix bug"}}}, events)

	for i := 0; i < 3; i++ {
		got := waitForEventType(t, events, CompletionGateEvaluated{})
		if got.Status != CompletionGateBlocked {
			t.Fatalf("CompletionGateEvaluated = %+v, want blocked", got)
		}
	}
	if len(fourthRequest) == 0 || !strings.Contains(fourthRequest[len(fourthRequest)-1].TextContent(), "Do not answer with plain text") {
		t.Fatalf("fourthRequest = %+v, want escalated no-progress reminder", fourthRequest)
	}
	if got := waitForEventType(t, events, CompletionGateEvaluated{}); got.Status != CompletionGatePassed {
		t.Fatalf("CompletionGateEvaluated = %+v, want passed", got)
	}
}

func TestTurnRunnerPreflightCompactsAndRefreshesSnapshot(t *testing.T) {
	original := []Message{{Role: UserRole, Content: strings.Repeat("x ", 4000)}}
	compacted := []Message{{Role: UserRole, Content: "summary"}}
	requestedMaxTokens := 4096
	contextWindow := EstimateRequestTokens(SystemPrompt{}, original, nil) + requestedMaxTokens + contextBudgetSafetyMargin - 1

	compactCalls := 0
	var streamMessages []Message
	var streamMaxTokens int
	compactCallsAtStream := -1
	client := &mockStreamClient{streamFn: func(_ context.Context, messages []Message, _ SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
		compactCallsAtStream = compactCalls
		streamMessages = append([]Message(nil), messages...)
		streamMaxTokens = maxTokens
		return textStream("ok"), nil
	}}

	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		requestedMaxTokens,
		TurnDeps{
			AppendMessage:     func(Message) {},
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			TryAutoCompact: func(context.Context) bool {
				compactCalls++
				return true
			},
			HistorySnapshot:   func() []Message { return append([]Message(nil), compacted...) },
			IncrementAPICalls: func() {},
			ContextWindow:     contextWindow,
		},
	)

	runner.Run(context.Background(), TurnPlan{Messages: original}, make(chan Event, 64))

	if compactCallsAtStream != 1 {
		t.Fatalf("compactCallsAtStream = %d, want 1", compactCallsAtStream)
	}
	if len(streamMessages) != 1 || streamMessages[0].Content != "summary" {
		t.Fatalf("streamMessages = %+v, want compacted snapshot", streamMessages)
	}
	if streamMaxTokens != requestedMaxTokens {
		t.Fatalf("stream maxTokens = %d, want %d", streamMaxTokens, requestedMaxTokens)
	}
}

func TestTurnRunnerPreflightShrinksMaxTokensToFitBudget(t *testing.T) {
	messages := []Message{{Role: UserRole, Content: strings.Repeat("x ", 4000)}}
	requestedMaxTokens := 4096
	wantMaxTokens := minContextBudgetMaxTokens + 500
	contextWindow := EstimateRequestTokens(SystemPrompt{}, messages, nil) + wantMaxTokens + contextBudgetSafetyMargin

	compactCalls := 0
	var streamMaxTokens int
	client := &mockStreamClient{streamFn: func(_ context.Context, _ []Message, _ SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
		streamMaxTokens = maxTokens
		return textStream("ok"), nil
	}}

	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		requestedMaxTokens,
		TurnDeps{
			AppendMessage:     func(Message) {},
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			TryAutoCompact: func(context.Context) bool {
				compactCalls++
				return false
			},
			HistorySnapshot:   func() []Message { return append([]Message(nil), messages...) },
			IncrementAPICalls: func() {},
			ContextWindow:     contextWindow,
		},
	)

	runner.Run(context.Background(), TurnPlan{Messages: messages}, make(chan Event, 64))

	if compactCalls != 2 {
		t.Fatalf("compactCalls = %d, want 2", compactCalls)
	}
	if streamMaxTokens != wantMaxTokens {
		t.Fatalf("stream maxTokens = %d, want %d", streamMaxTokens, wantMaxTokens)
	}
}

func TestTurnRunnerPreflightKeepsMaxTokensWhenBudgetFits(t *testing.T) {
	messages := []Message{{Role: UserRole, Content: "small"}}
	requestedMaxTokens := 4096
	contextWindow := EstimateRequestTokens(SystemPrompt{}, messages, nil) + requestedMaxTokens + contextBudgetSafetyMargin

	compactCalls := 0
	var streamMaxTokens int
	client := &mockStreamClient{streamFn: func(_ context.Context, _ []Message, _ SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
		streamMaxTokens = maxTokens
		return textStream("ok"), nil
	}}

	runner := NewTurnRunner(
		NewModelStreamer(client, tool.NewRegistry(), nil),
		nil,
		nil,
		requestedMaxTokens,
		TurnDeps{
			AppendMessage:     func(Message) {},
			PersistMessage:    func(context.Context, Message) {},
			UpdateSessionMeta: func(context.Context, modelResponse) {},
			DrainQueuedInputs: func() []string { return nil },
			TryAutoCompact: func(context.Context) bool {
				compactCalls++
				return false
			},
			HistorySnapshot:   func() []Message { return append([]Message(nil), messages...) },
			IncrementAPICalls: func() {},
			ContextWindow:     contextWindow,
		},
	)

	runner.Run(context.Background(), TurnPlan{Messages: messages}, make(chan Event, 64))

	if compactCalls != 1 {
		t.Fatalf("compactCalls = %d, want 1", compactCalls)
	}
	if streamMaxTokens != requestedMaxTokens {
		t.Fatalf("stream maxTokens = %d, want %d", streamMaxTokens, requestedMaxTokens)
	}
}

func textStream(text string) <-chan ApiStreamEvent {
	ch := make(chan ApiStreamEvent, 6)
	ch <- ApiStreamEvent{EventType: "message_start", InputTokens: 10}
	ch <- ApiStreamEvent{EventType: "content_block_start", Index: 0}
	ch <- ApiStreamEvent{EventType: "content_block_delta", Detail: "text_delta", Delta: text}
	ch <- ApiStreamEvent{EventType: "content_block_stop", Index: 0}
	ch <- ApiStreamEvent{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 1}
	ch <- ApiStreamEvent{Done: true, EventType: "message_stop"}
	close(ch)
	return ch
}
