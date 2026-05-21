package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cece/internal/prompt"
	"cece/internal/tool"
)

type fakeClient struct {
	chunks    []ApiStreamEvent
	maxTokens int // captured from Stream call
}

func (f *fakeClient) Stream(ctx context.Context, messages []Message, _ SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
	f.maxTokens = maxTokens
	out := make(chan ApiStreamEvent, len(f.chunks))
	for _, chunk := range f.chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func TestRuntimeSendEmitsEventsAndStoresAssistantReply(t *testing.T) {
	runtime := NewRuntime(&fakeClient{chunks: []ApiStreamEvent{
		{Delta: "Hello"},
		{Delta: " world"},
		{Done: true},
	}}, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}

	if len(collected) != 7 {
		t.Fatalf("len(collected) = %d, want 7", len(collected))
	}

	// Verify UIModelRequestStarted is emitted with reason "user"
	if req, ok := collected[1].(UIModelRequestStarted); !ok {
		t.Fatalf("collected[1] = %T, want UIModelRequestStarted", collected[1])
	} else if req.Reason != "user" {
		t.Fatalf("UIModelRequestStarted.Reason = %q, want %q", req.Reason, "user")
	}

	history := runtime.History()
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Role != UserRole || history[0].Content != "Hi" {
		t.Fatalf("unexpected user message: %#v", history[0])
	}
	if history[1].Role != AssistantRole || history[1].Content != "Hello world" {
		t.Fatalf("unexpected assistant message: %#v", history[1])
	}
}

func TestRuntimeSendEmitsRunFailedOnChunkError(t *testing.T) {
	runtime := NewRuntime(&fakeClient{chunks: []ApiStreamEvent{
		{Delta: "partial"},
		{Err: errors.New("stream broke")},
	}}, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var sawFailure bool
	for event := range events {
		if _, ok := event.(UIRunFailed); ok {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("expected UIRunFailed event")
	}

	history := runtime.History()
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
}

func TestRuntimeSendEmitsStreamStartedAndCompleted(t *testing.T) {
	runtime := NewRuntime(&fakeClient{chunks: []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 42},
		{Delta: "hi", EventType: "content_block_delta", Detail: "text_delta"},
		{EventType: "message_delta", Detail: "stop_reason", OutputTokens: 7, StopReason: "end_turn"},
		{Done: true},
	}}, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var started UIStreamStarted
	var completed UIStreamCompleted
	var detailCount int
	for event := range events {
		switch e := event.(type) {
		case UIStreamStarted:
			started = e
		case UIStreamCompleted:
			completed = e
		case UIStreamEventDetail:
			detailCount++
		}
	}

	if started.InputTokens != 42 {
		t.Fatalf("UIStreamStarted.InputTokens = %d, want 42", started.InputTokens)
	}
	if completed.OutputTokens != 7 {
		t.Fatalf("UIStreamCompleted.OutputTokens = %d, want 7", completed.OutputTokens)
	}
	if completed.StopReason != "end_turn" {
		t.Fatalf("UIStreamCompleted.StopReason = %q, want %q", completed.StopReason, "end_turn")
	}
	if completed.Duration == 0 {
		t.Fatal("UIStreamCompleted.Duration should be > 0")
	}
}

func TestRuntimeSendEmitsStreamEventDetails(t *testing.T) {
	runtime := NewRuntime(&fakeClient{chunks: []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 10},
		{Delta: "Hel", EventType: "content_block_delta", Detail: "text_delta"},
		{Delta: "lo", EventType: "content_block_delta", Detail: "text_delta"},
		{EventType: "message_delta", Detail: "stop_reason", OutputTokens: 3, StopReason: "end_turn"},
		{Done: true},
	}}, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var details []UIStreamEventDetail
	for event := range events {
		if e, ok := event.(UIStreamEventDetail); ok {
			details = append(details, e)
		}
	}

	if len(details) != 4 {
		t.Fatalf("got %d UIStreamEventDetail events, want 4", len(details))
	}

	if details[0].EventType != "message_start" {
		t.Fatalf("details[0].EventType = %q, want %q", details[0].EventType, "message_start")
	}
	if details[1].EventType != "content_block_delta" || details[1].Detail != "text_delta" {
		t.Fatalf("details[1] = %+v, want content_block_delta/text_delta", details[1])
	}
	if details[1].Text != "Hel" {
		t.Fatalf("details[1].Text = %q, want %q", details[1].Text, "Hel")
	}
	if details[2].Text != "lo" {
		t.Fatalf("details[2].Text = %q, want %q", details[2].Text, "lo")
	}
	if details[3].EventType != "message_delta" {
		t.Fatalf("details[3].EventType = %q, want %q", details[3].EventType, "message_delta")
	}
}

// multiResponseClient returns different chunk sets on successive Stream calls.
type multiResponseClient struct {
	responses  [][]ApiStreamEvent
	callCount  int
	lastMaxTokens int // captured from most recent Stream call
}

func (m *multiResponseClient) Stream(ctx context.Context, messages []Message, _ SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error) {
	chunks := m.responses[m.callCount]
	m.lastMaxTokens = maxTokens
	m.callCount++
	out := make(chan ApiStreamEvent, len(chunks))
	for _, chunk := range chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

// stubTool is a minimal tool for testing tool execution flows.
type stubTool struct{}

func (stubTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Stub",
		Description: "stub for testing",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (stubTool) Run(_ context.Context, _ json.RawMessage, _ tool.Emitter) tool.Result {
	return tool.Result{Content: "stub result"}
}

func toolUseChunks(toolID, toolName, input string) []ApiStreamEvent {
	return []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 10},
		{EventType: "content_block_start", ToolCallID: toolID, ToolCallName: toolName, Index: 0},
		{Detail: "input_json_delta", ToolCallInput: input, Index: 0},
		{EventType: "content_block_stop", Index: 0},
		{EventType: "message_delta", StopReason: "tool_use", OutputTokens: 5},
		{Done: true},
	}
}

func endTurnChunks(text string) []ApiStreamEvent {
	return []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 5},
		{Delta: text, EventType: "content_block_delta", Detail: "text_delta"},
		{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 3},
		{Done: true},
	}
}

func maxTokensChunks() []ApiStreamEvent {
	return []ApiStreamEvent{
		{EventType: "message_start", InputTokens: 5},
		{Delta: "truncated output...", EventType: "content_block_delta", Detail: "text_delta"},
		{EventType: "message_delta", StopReason: "max_tokens", OutputTokens: 16384},
		{Done: true},
	}
}

func TestRuntimeYoloModeSkipsConfirmation(t *testing.T) {
	registry := tool.NewRegistry(stubTool{})
	client := &multiResponseClient{
		responses: [][]ApiStreamEvent{
			toolUseChunks("call_1", "Stub", "{}"),
			endTurnChunks("done"),
		},
	}
	runtime := NewRuntime(client, registry, true, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Do something")
	if err != nil {
		t.Fatalf("Input returned error: %v", err)
	}

	var sawCallsReady, sawExecCompleted, sawAssistantCompleted bool
	for event := range events {
		switch e := event.(type) {
		case UIToolCallsReady:
			sawCallsReady = true
		case UIToolExecCompleted:
			sawExecCompleted = true
			if e.Result.Content != "stub result" {
				t.Fatalf("tool result = %q, want %q", e.Result.Content, "stub result")
			}
		case UIAssistantCompleted:
			sawAssistantCompleted = true
		}
	}

	if sawCallsReady {
		t.Fatal("UIToolCallsReady should NOT be emitted in yolo mode")
	}
	if !sawExecCompleted {
		t.Fatal("UIToolExecCompleted should be emitted in yolo mode")
	}
	if !sawAssistantCompleted {
		t.Fatal("UIAssistantCompleted should be emitted in yolo mode")
	}
}

func TestRuntimeNormalModeRequiresConfirmation(t *testing.T) {
	registry := tool.NewRegistry(stubTool{})
	client := &multiResponseClient{
		responses: [][]ApiStreamEvent{
			toolUseChunks("call_1", "Stub", "{}"),
			endTurnChunks("done"),
		},
	}
	runtime := NewRuntime(client, registry, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Do something")
	if err != nil {
		t.Fatalf("Input returned error: %v", err)
	}

	// Consume events in a goroutine; collect UIToolCallsReady then confirm.
	eventCh := make(chan Event, 64)
	go func() {
		for e := range events {
			eventCh <- e
		}
		close(eventCh)
	}()

	// Wait for UIToolCallsReady — it should be emitted before Confirm().
	var sawCallsReady bool
	for e := range eventCh {
		if _, ok := e.(UIToolCallsReady); ok {
			sawCallsReady = true
			runtime.Confirm()
			break
		}
	}
	if !sawCallsReady {
		t.Fatal("UIToolCallsReady should be emitted in normal mode")
	}

	// Drain remaining events and verify tool executed.
	var sawExecCompleted bool
	for e := range eventCh {
		if _, ok := e.(UIToolExecCompleted); ok {
			sawExecCompleted = true
		}
	}
	if !sawExecCompleted {
		t.Fatal("UIToolExecCompleted should be emitted after Confirm()")
	}
}

func TestRuntimeToolResultTriggersModelRequestWithToolResultReason(t *testing.T) {
	registry := tool.NewRegistry(stubTool{})
	client := &multiResponseClient{
		responses: [][]ApiStreamEvent{
			toolUseChunks("call_1", "Stub", "{}"),
			endTurnChunks("done"),
		},
	}
	runtime := NewRuntime(client, registry, true, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Do something")
	if err != nil {
		t.Fatalf("Input returned error: %v", err)
	}

	var requestReasons []string
	for event := range events {
		if e, ok := event.(UIModelRequestStarted); ok {
			requestReasons = append(requestReasons, e.Reason)
		}
	}

	if len(requestReasons) != 2 {
		t.Fatalf("got %d UIModelRequestStarted events, want 2", len(requestReasons))
	}
	if requestReasons[0] != "user" {
		t.Fatalf("first request reason = %q, want %q", requestReasons[0], "user")
	}
	if requestReasons[1] != "tool_result" {
		t.Fatalf("second request reason = %q, want %q", requestReasons[1], "tool_result")
	}
}

func TestStreamEventDetailTextTruncation(t *testing.T) {
	runtime := NewRuntime(&fakeClient{chunks: []ApiStreamEvent{
		{Delta: "this is a very long text that exceeds twenty characters", EventType: "content_block_delta", Detail: "text_delta"},
		{Done: true},
	}}, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	for event := range events {
		if e, ok := event.(UIStreamEventDetail); ok && e.EventType == "content_block_delta" {
			if len(e.Text) > 60 {
				t.Fatalf("UIStreamEventDetail.Text = %q (len=%d), want len <= 60", e.Text, len(e.Text))
			}
			return
		}
	}
	t.Fatal("expected UIStreamEventDetail for content_block_delta")
}

func TestRuntimeTruncationRetryEscalatesMaxTokens(t *testing.T) {
	client := &multiResponseClient{
		responses: [][]ApiStreamEvent{
			maxTokensChunks(),      // first call: stop_reason = "max_tokens"
			endTurnChunks("full"),  // retry with escalated max_tokens: success
		},
	}
	runtime := NewRuntime(client, nil, true, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Long answer please")
	if err != nil {
		t.Fatalf("Input returned error: %v", err)
	}

	var sawRetry UITruncationRetry
	var sawRetryEvent bool
	var sawAssistantCompleted bool
	for event := range events {
		switch e := event.(type) {
		case UITruncationRetry:
			sawRetry = e
			sawRetryEvent = true
		case UIAssistantCompleted:
			sawAssistantCompleted = true
		}
	}

	if !sawRetryEvent {
		t.Fatal("expected UITruncationRetry event")
	}
	if sawRetry.Attempt != 1 {
		t.Fatalf("UITruncationRetry.Attempt = %d, want 1", sawRetry.Attempt)
	}
	if sawRetry.PrevMaxTokens != 16384 {
		t.Fatalf("UITruncationRetry.PrevMaxTokens = %d, want 16384", sawRetry.PrevMaxTokens)
	}
	if sawRetry.NewMaxTokens != 64000 {
		t.Fatalf("UITruncationRetry.NewMaxTokens = %d, want 64000", sawRetry.NewMaxTokens)
	}
	if !sawAssistantCompleted {
		t.Fatal("expected UIAssistantCompleted after retry")
	}

	history := runtime.History()
	// user message + assistant message (from retry, not the truncated one)
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[1].Role != AssistantRole || history[1].Content != "full" {
		t.Fatalf("assistant message = %#v, want content 'full'", history[1])
	}
}

func TestRuntimePassesMaxTokensToStream(t *testing.T) {
	fc := &fakeClient{chunks: []ApiStreamEvent{
		{Delta: "ok"},
		{Done: true},
	}}
	runtime := NewRuntime(fc, nil, false, 16384, nil, "")

	events, err := runtime.Input(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("Input returned error: %v", err)
	}
	for range events {
	}

	if fc.maxTokens != 16384 {
		t.Fatalf("Stream called with maxTokens = %d, want 16384", fc.maxTokens)
	}
}

// switchableFakeClient implements ModelClient + modelSetter + modelLister.
type switchableFakeClient struct {
	fakeClient
	model  string
	models []ModelInfo
}

func (s *switchableFakeClient) SetModel(model string) { s.model = model }
func (s *switchableFakeClient) Model() string         { return s.model }

func (s *switchableFakeClient) ListModels(_ context.Context) ([]ModelInfo, error) {
	if s.models == nil {
		return nil, errors.New("no models")
	}
	return s.models, nil
}

func TestRuntimeSwitchModel(t *testing.T) {
	client := &switchableFakeClient{model: "old-model"}
	assembler := prompt.NewContextAssembler("", nil, nil)
	runtime := NewRuntime(client, nil, false, 16384, assembler, "")

	runtime.SwitchModel("new-model", 200000, "", "", "", "", "", "")

	if client.model != "new-model" {
		t.Fatalf("client.model = %q, want %q", client.model, "new-model")
	}
}

func TestRuntimeListAllModels(t *testing.T) {
	expected := []ModelInfo{
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", MaxContextWindow: 200000},
		{ID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", MaxContextWindow: 200000},
	}
	runtime := NewRuntime(nil, nil, false, 16384, nil, "")
	runtime.SetListAllModelsFn(func(ctx context.Context) ([]ModelInfo, error) {
		return expected, nil
	})

	models, err := runtime.ListAllModels(context.Background())
	if err != nil {
		t.Fatalf("ListAllModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "claude-sonnet-4-6" {
		t.Fatalf("models[0].ID = %q, want %q", models[0].ID, "claude-sonnet-4-6")
	}
}

func TestRuntimeListAllModelsNotConfigured(t *testing.T) {
	runtime := NewRuntime(nil, nil, false, 16384, nil, "")

	_, err := runtime.ListAllModels(context.Background())
	if err == nil {
		t.Fatal("ListAllModels should return error when not configured")
	}
}
