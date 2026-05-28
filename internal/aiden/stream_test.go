package aiden

import (
	"io"
	"strings"
	"testing"

	"cece/internal/chat"
)

// helper: build SSE body from lines
func sseBody(lines ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

func collectEvents(ch <-chan chat.ApiStreamEvent) ([]chat.ApiStreamEvent, error) {
	var events []chat.ApiStreamEvent
	for e := range ch {
		if e.Err != nil {
			return events, e.Err
		}
		events = append(events, e)
	}
	return events, nil
}

func TestDecodeTextDeltas(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		``,
		`data: {"id":"2","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	var doneSeen bool
	for _, e := range events {
		if e.Done {
			doneSeen = true
		}
		if e.Delta != "" {
			text += e.Delta
		}
	}
	if text != "Hello" {
		t.Errorf("expected text 'Hello', got %q", text)
	}
	if !doneSeen {
		t.Error("expected Done event")
	}
}

func TestDecodeDoneSentinel(t *testing.T) {
	body := sseBody(
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Done {
		t.Error("expected Done=true")
	}
}

func TestDecodeMessageStartWithUsage(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":42,"completion_tokens":0,"total_tokens":42}}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var startEvent *chat.ApiStreamEvent
	for i := range events {
		if events[i].EventType == "message_start" {
			startEvent = &events[i]
			break
		}
	}
	if startEvent == nil {
		t.Fatal("expected message_start event")
	}
	if startEvent.InputTokens != 42 {
		t.Errorf("expected InputTokens=42, got %d", startEvent.InputTokens)
	}
}

func TestDecodeFinishReasonStop(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range events {
		if e.EventType == "message_delta" {
			if e.StopReason != "end_turn" {
				t.Errorf("expected stop_reason 'end_turn', got %q", e.StopReason)
			}
			return
		}
	}
	t.Fatal("expected message_delta event")
}

func TestDecodeFinishReasonToolCalls(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range events {
		if e.EventType == "message_delta" {
			if e.StopReason != "tool_use" {
				t.Errorf("expected stop_reason 'tool_use', got %q", e.StopReason)
			}
			return
		}
	}
	t.Fatal("expected message_delta event")
}

func TestDecodeToolCallStartAndDelta(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"Bash"}}]}}]}`,
		``,
		`data: {"id":"2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd"}}]}}]}`,
		``,
		`data: {"id":"3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"ls\"}"}}]}}]}`,
		``,
		`data: {"id":"4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: message_start, content_block_start, input_json_delta x2, content_block_stop, message_delta, done
	var startSeen, blockStopSeen bool
	var toolName, toolID string
	var inputParts []string

	for _, e := range events {
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			startSeen = true
			toolID = e.ToolCallID
			toolName = e.ToolCallName
		}
		if e.Detail == "input_json_delta" {
			inputParts = append(inputParts, e.ToolCallInput)
		}
		if e.EventType == "content_block_stop" {
			blockStopSeen = true
		}
	}

	if !startSeen {
		t.Error("expected content_block_start for tool call")
	}
	if toolID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %q", toolID)
	}
	if toolName != "Bash" {
		t.Errorf("expected tool_call_name 'Bash', got %q", toolName)
	}
	if !blockStopSeen {
		t.Error("expected content_block_stop (synthesized)")
	}
	combined := strings.Join(inputParts, "")
	if combined != `{"cmd":"ls"}` {
		t.Errorf("expected combined input '{\"cmd\":\"ls\"}', got %q", combined)
	}
}

func TestDecodeResponsesFunctionCallStartAndDelta(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"file_path"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\":\"/tmp/x\"}"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":3}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolName, toolID string
	var inputParts []string
	var stopReason string
	var doneSeen bool
	for _, e := range events {
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			toolID = e.ToolCallID
			toolName = e.ToolCallName
		}
		if e.Detail == "input_json_delta" {
			inputParts = append(inputParts, e.ToolCallInput)
		}
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
		if e.Done {
			doneSeen = true
		}
	}

	if toolID != "call_1" {
		t.Fatalf("tool id = %q, want call_1", toolID)
	}
	if toolName != "Read" {
		t.Fatalf("tool name = %q, want Read", toolName)
	}
	if got := strings.Join(inputParts, ""); got != `{"file_path":"/tmp/x"}` {
		t.Fatalf("tool input = %q, want file_path payload", got)
	}
	if stopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", stopReason)
	}
	if !doneSeen {
		t.Fatal("expected Done event")
	}
}

func TestDecodeReasoningContent(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"reasoning_content":"let me think"}}]}`,
		``,
		`data: {"id":"2","choices":[{"index":0,"delta":{"reasoning_content":" more"}}]}`,
		``,
		`data: {"id":"3","choices":[{"index":0,"delta":{"content":"answer"}}]}`,
		``,
		`data: {"id":"4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var thinkingText string
	var textContent string
	var thinkingBlockStopSeen bool

	for _, e := range events {
		if e.ThinkingDelta != "" {
			thinkingText += e.ThinkingDelta
		}
		if e.Delta != "" {
			textContent += e.Delta
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingBlockStopSeen = true
		}
	}

	if thinkingText != "let me think more" {
		t.Errorf("expected thinking text 'let me think more', got %q", thinkingText)
	}
	if textContent != "answer" {
		t.Errorf("expected text content 'answer', got %q", textContent)
	}
	if !thinkingBlockStopSeen {
		t.Error("expected thinking content_block_stop when transitioning to text")
	}
}

func TestDecodeFinishReasonLength(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range events {
		if e.EventType == "message_delta" {
			if e.StopReason != "max_tokens" {
				t.Errorf("expected stop_reason 'max_tokens', got %q", e.StopReason)
			}
			return
		}
	}
	t.Fatal("expected message_delta event")
}

func TestDecodeErrorChunk(t *testing.T) {
	body := sseBody(
		`data: {invalid json}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	// err comes from the Err event sent by the parser
	if err == nil {
		t.Error("expected error from malformed JSON")
	}
	_ = events
}

func TestMapStopReason(t *testing.T) {
	tests := []struct{ in, want string }{
		{"stop", "end_turn"},
		{"tool_calls", "tool_use"},
		{"length", "max_tokens"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := mapStopReason(tt.in)
		if got != tt.want {
			t.Errorf("mapStopReason(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecodeCachedTokensChatCompletions(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
		``,
		`data: {"id":"2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12000,"completion_tokens":5,"total_tokens":12005,"prompt_tokens_details":{"cached_tokens":9000}}}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cacheRead int
	for _, e := range events {
		if e.EventType == "message_start" && e.InputTokens > 0 {
			cacheRead = e.CacheReadTokens
		}
	}
	if cacheRead != 9000 {
		t.Errorf("expected CacheReadTokens=9000, got %d", cacheRead)
	}
}

func TestDecodeCachedTokensResponsesAPI(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12000,"output_tokens":3,"total_tokens":12003,"input_tokens_details":{"cached_tokens":7500}}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cacheRead int
	for _, e := range events {
		if e.EventType == "message_start" && e.InputTokens > 0 {
			cacheRead = e.CacheReadTokens
		}
	}
	if cacheRead != 7500 {
		t.Errorf("expected CacheReadTokens=7500, got %d", cacheRead)
	}
}

func TestDecodeCachedTokensAidenNormalized(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12000,"output_tokens":3,"total_tokens":12003,"input_token_details":{"cache_read":6000}}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var cacheRead int
	for _, e := range events {
		if e.EventType == "message_start" && e.InputTokens > 0 {
			cacheRead = e.CacheReadTokens
		}
	}
	if cacheRead != 6000 {
		t.Errorf("expected CacheReadTokens=6000, got %d", cacheRead)
	}
}
