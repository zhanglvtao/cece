package aiden

import (
	"io"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

// helper: build SSE body from lines
func sseBody(lines ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

func collectEvents(ch <-chan agent.ApiStreamEvent) ([]agent.ApiStreamEvent, error) {
	var events []agent.ApiStreamEvent
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
		`data: {"id":"1","object":"agent.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		``,
		`data: {"id":"2","object":"agent.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
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
	var startEvent *agent.ApiStreamEvent
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

func TestDecodeFinishReasonStopEOFWithoutDoneSynthesizesDone(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
		``,
		`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var doneSeen bool
	var stopReason string
	for _, e := range events {
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
		if e.Done {
			doneSeen = true
		}
	}
	if stopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn", stopReason)
	}
	if !doneSeen {
		t.Fatal("expected Done event when chat-completions stream ends after terminal finish_reason")
	}
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
			if e.ToolCallProviderID != "fc_1" {
				t.Fatalf("provider id = %q, want fc_1", e.ToolCallProviderID)
			}
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
		if e.EventType == "message_delta" && e.InputTokens > 0 {
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
		if e.EventType == "message_delta" && e.InputTokens > 0 {
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
		if e.EventType == "message_delta" && e.InputTokens > 0 {
			cacheRead = e.CacheReadTokens
		}
	}
	if cacheRead != 6000 {
		t.Errorf("expected CacheReadTokens=6000, got %d", cacheRead)
	}
}

func TestDecodeResponsesOutputTextDone(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created"}`,
		``,
		`data: {"type":"response.output_text.done","content_index":0,"output_index":1,"text":"hello from done"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12,"output_tokens":4}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	var blockStarted bool
	for _, e := range events {
		if e.EventType == "content_block_start" && e.Index == 1 {
			blockStarted = true
		}
		if e.Detail == "text_delta" {
			text += e.Delta
		}
	}

	if !blockStarted {
		t.Fatal("expected content_block_start for output_text.done")
	}
	if text != "hello from done" {
		t.Fatalf("text = %q, want output_text.done text", text)
	}
}

func TestDecodeResponsesOutputTextDoneAfterDeltaDoesNotDuplicate(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created"}`,
		``,
		`data: {"type":"response.output_text.delta","content_index":0,"output_index":0,"delta":"hello"}`,
		``,
		`data: {"type":"response.output_text.done","content_index":0,"output_index":0,"text":"hello"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12,"output_tokens":4}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	var deltas int
	for _, e := range events {
		if e.Detail == "text_delta" {
			deltas++
			text += e.Delta
		}
	}

	if deltas != 1 {
		t.Fatalf("text deltas = %d, want 1", deltas)
	}
	if text != "hello" {
		t.Fatalf("text = %q, want delta text only", text)
	}
}

func TestDecodeDeepSeekStyleUsageChunk(t *testing.T) {
	// Simulates DeepSeek's actual stream pattern:
	// 1. content chunks with reasoning_content
	// 2. finish_reason:stop chunk (choices non-empty, usage=0)
	// 3. usage-only chunk (choices=[], usage populated)
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"hello"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
		``,
		`data: {"id":"2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
		``,
		`data: {"id":"3","choices":[],"usage":{"prompt_tokens":160,"completion_tokens":1088,"total_tokens":1248,"prompt_tokens_details":{"cached_tokens":0}}}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inputTokens, outputTokens int
	var messageDeltas int
	for _, e := range events {
		if e.EventType == "message_delta" {
			messageDeltas++
			if e.InputTokens > 0 {
				inputTokens = e.InputTokens
			}
			if e.OutputTokens > 0 {
				outputTokens = e.OutputTokens
			}
		}
	}

	if inputTokens != 160 {
		t.Errorf("expected InputTokens=160, got %d", inputTokens)
	}
	if outputTokens != 1088 {
		t.Errorf("expected OutputTokens=1088, got %d", outputTokens)
	}
	if messageDeltas < 2 {
		t.Errorf("expected at least 2 message_delta events (finish + usage), got %d", messageDeltas)
	}
}

func TestDecodeDeepSeekStyleUsageOnlyChunk(t *testing.T) {
	body := sseBody(
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
		``,
		`data: {"id":"2","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":10,"total_tokens":60,"prompt_tokens_details":{"cached_tokens":0}}}`,
		``,
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inputTokens, outputTokens int
	for _, e := range events {
		if e.EventType == "message_delta" {
			if e.InputTokens > 0 {
				inputTokens = e.InputTokens
			}
			if e.OutputTokens > 0 {
				outputTokens = e.OutputTokens
			}
		}
	}

	if inputTokens != 50 {
		t.Errorf("expected InputTokens=50, got %d", inputTokens)
	}
	if outputTokens != 10 {
		t.Errorf("expected OutputTokens=10, got %d", outputTokens)
	}
}

func TestDecodeResponsesReasoningEvents(t *testing.T) {
	// Simulates gpt-5.4 Responses API with reasoning output items
	// that emit reasoning_text.delta events instead of output_text.delta.
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Let me think"}]}}`,
		``,
		`data: {"type":"response.reasoning_text.delta","output_index":0,"delta":" about this"}`,
		``,
		`data: {"type":"response.reasoning_text.done","output_index":0}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var thinkingStart, thinkingStop bool
	var thinkingText string
	var stopReason string
	for _, e := range events {
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingStart = true
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingStop = true
		}
		if e.Detail == "thinking_delta" {
			thinkingText += e.ThinkingDelta
		}
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
	}

	if !thinkingStart {
		t.Error("expected content_block_start with IsThinking for reasoning item")
	}
	if !thinkingStop {
		t.Error("expected content_block_stop with IsThinking for reasoning item")
	}
	if thinkingText != "Let me think about this" {
		t.Errorf("thinking text = %q, want 'Let me think about this'", thinkingText)
	}
	if stopReason != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn", stopReason)
	}
}

func TestDecodeResponsesReasoningSummaryText(t *testing.T) {
	// Simulates reasoning_summary_text events (separate from reasoning_text).
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"summary"}`,
		``,
		`data: {"type":"response.reasoning_summary_text.done","output_index":0}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var thinkingText string
	for _, e := range events {
		if e.Detail == "thinking_delta" {
			thinkingText += e.ThinkingDelta
		}
	}
	if thinkingText != "summary" {
		t.Errorf("thinking text = %q, want 'summary'", thinkingText)
	}
}

func TestDecodeResponsesCompletedWithNoOutput(t *testing.T) {
	// Simulates the exact scenario of c25b5055: response.completed without any
	// text or tool call output. The stream should still emit Done properly.
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":0}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var doneSeen bool
	var stopReason string
	for _, e := range events {
		if e.Done {
			doneSeen = true
		}
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
	}
	if !doneSeen {
		t.Error("expected Done event")
	}
	if stopReason != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn", stopReason)
	}
}

// Test: reasoning + function_call round-trip.
// When the API returns a reasoning item followed by a function_call item,
// both must be correctly decoded so they can be serialized back in the
// next request's input.
func TestDecodeResponsesReasoningThenFunctionCall(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_abc","summary":[{"type":"summary_text","text":"thinking..."}],"encrypted_content":"ENC123"}}`,
		``,
		`data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"hmm"}`,
		``,
		`data: {"type":"response.reasoning_text.done","output_index":0}`,
		``,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"file_path"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"\":\"/tmp\"}"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_abc"}}`,
		``,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Read","arguments":"{\"file_path\":\"/tmp\"}"}}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect thinking block info
	var thinkingStartEvent, thinkingStopEvent *agent.ApiStreamEvent
	var thinkingText string
	var thinkingProviderID string
	var thinkingEncryptedContent string
	// Collect tool call info
	var toolStartEvent *agent.ApiStreamEvent
	var toolInputParts []string
	var stopReason string

	for i := range events {
		e := &events[i]
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingStartEvent = e
			thinkingProviderID = e.ThinkingProviderID
			thinkingEncryptedContent = e.ThinkingEncryptedContent
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingStopEvent = e
		}
		if e.Detail == "thinking_delta" {
			thinkingText += e.ThinkingDelta
		}
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			toolStartEvent = e
		}
		if e.Detail == "input_json_delta" {
			toolInputParts = append(toolInputParts, e.ToolCallInput)
		}
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
	}

	// Verify thinking block
	if thinkingStartEvent == nil {
		t.Fatal("expected content_block_start for reasoning item")
	}
	if thinkingProviderID != "rs_abc" {
		t.Errorf("thinking provider ID = %q, want rs_abc", thinkingProviderID)
	}
	if thinkingEncryptedContent != "ENC123" {
		t.Errorf("thinking encrypted content = %q, want ENC123", thinkingEncryptedContent)
	}
	if thinkingStopEvent == nil {
		t.Fatal("expected content_block_stop for reasoning item")
	}
	if thinkingText != "thinking...hmm" {
		t.Errorf("thinking text = %q, want 'thinking...hmm'", thinkingText)
	}

	// Verify function call
	if toolStartEvent == nil {
		t.Fatal("expected content_block_start for function_call item")
	}
	if toolStartEvent.ToolCallProviderID != "fc_1" {
		t.Errorf("tool call provider ID = %q, want fc_1", toolStartEvent.ToolCallProviderID)
	}
	toolInput := strings.Join(toolInputParts, "")
	if toolInput != `{"file_path":"/tmp"}` {
		t.Errorf("tool input = %q, want file_path payload", toolInput)
	}

	if stopReason != "tool_use" {
		t.Errorf("stop reason = %q, want tool_use", stopReason)
	}
}

// Test: API sends reasoning_text.delta WITHOUT prior output_item.added.
// With the fallback logic, a reasoning block is created on the fly
// from the first reasoning_text.delta event.
func TestDecodeResponsesReasoningTextWithoutOutputItemAdded(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		// NOTE: no response.output_item.added for reasoning!
		`data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"thinking"}`,
		``,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Bash","arguments":""}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{}"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Bash","arguments":"{}"}}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With the fallback, reasoning_text.delta creates the reasoning block on the fly.
	var toolStartSeen, thinkingStartSeen bool
	for _, e := range events {
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingStartSeen = true
		}
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			toolStartSeen = true
		}
	}
	if !thinkingStartSeen {
		t.Error("expected thinking content_block_start — fallback should create reasoning block from reasoning_text.delta")
	}
	if !toolStartSeen {
		t.Error("expected function_call content_block_start")
	}
}
