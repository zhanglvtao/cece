package codebase

import (
	"io"
	"strings"
	"testing"

	"cece/internal/agent"
)
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

func TestDecodeTextResponse(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"Hel"}`,
		``,
		`event: output`,
		`data: {"response":"lo"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
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

func TestDecodeMessageStartFromFirstOutput(t *testing.T) {
	body := sseBody(
		`event: token_usage`,
		`data: {"prompt_tokens":42,"completion_tokens":0}`,
		``,
		`event: output`,
		`data: {"response":"hi"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
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
		`event: output`,
		`data: {"response":"done"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
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
		`event: output`,
		`data: {"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]}`,
		``,
		`event: done`,
		`data: {"finish_reason":"tool_calls"}`,
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
		`event: output`,
		`data: {"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash"}}]}`,
		``,
		`event: output`,
		`data: {"tool_calls":[{"index":0,"type":"function","function_call":{"arguments":"{\"cmd"}}]}`,
		``,
		`event: output`,
		`data: {"tool_calls":[{"index":0,"type":"function","function_call":{"arguments":"\":\"ls\"}"}}]}`,
		``,
		`event: done`,
		`data: {"finish_reason":"tool_calls"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestDecodeReasoningContent(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"reasoning_content":"let me think"}`,
		``,
		`event: output`,
		`data: {"reasoning_content":" more"}`,
		``,
		`event: output`,
		`data: {"response":"answer"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
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
		`event: output`,
		`data: {"response":"truncated"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"length"}`,
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
		`event: output`,
		`data: {invalid json}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
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

func TestDecodeTokenUsageEvent(t *testing.T) {
	body := sseBody(
		`event: token_usage`,
		`data: {"prompt_tokens":100,"completion_tokens":50}`,
		``,
		`event: output`,
		`data: {"response":"hi"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var delta *agent.ApiStreamEvent
	for i := range events {
		if events[i].EventType == "message_delta" {
			delta = &events[i]
			break
		}
	}
	if delta == nil {
		t.Fatal("expected message_delta event")
	}
	if delta.OutputTokens != 50 {
		t.Errorf("expected OutputTokens=50, got %d", delta.OutputTokens)
	}
}

func TestDecodeMetadataEvent(t *testing.T) {
	body := sseBody(
		`event: metadata`,
		`data: {"model":"some-model"}`,
		``,
		`event: output`,
		`data: {"response":"ok"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// metadata event should be silently ignored
	var text string
	for _, e := range events {
		if e.Delta != "" {
			text += e.Delta
		}
	}
	if text != "ok" {
		t.Errorf("expected text 'ok', got %q", text)
	}
}

func TestDecodeFullEventSequence(t *testing.T) {
	// Verify the complete event sequence: message_start → content_block_start → content_block_delta → content_block_stop → message_delta → done
	body := sseBody(
		`event: output`,
		`data: {"response":"Hello"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", ""}
	var actual []string
	for _, e := range events {
		if e.Done {
			actual = append(actual, "")
		} else {
			actual = append(actual, e.EventType)
		}
	}

	if len(actual) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(actual), actual)
	}
	for i, want := range expected {
		if actual[i] != want {
			t.Errorf("event[%d]: expected %q, got %q", i, want, actual[i])
		}
	}
}

func TestDecodeErrorEvent(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"hi"}`,
		``,
		`event: error`,
		`data: {"message":"rate limit exceeded","code":"429"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err == nil {
		t.Fatal("expected error from error event")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("expected error to contain 'rate limit exceeded', got %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected error to contain code '429', got %v", err)
	}
	_ = events
}

func TestDecodeErrorEventWithNumericCode(t *testing.T) {
	body := sseBody(
		`event: error`,
		`data: {"code":4001,"error":"biz error: rpc error: code = ErrParamInvalid desc = invalid param, origin err = app is not found","message":"trae_permanent_error(invalid params): invalid param"}`,
		``,
	)

	_, err := collectEvents(DecodeStreamEvent(body))
	if err == nil {
		t.Fatal("expected error from numeric-code error event")
	}
	if strings.Contains(err.Error(), "parse failure") {
		t.Fatalf("expected business error, got parse failure: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid param") {
		t.Fatalf("expected business error message to be preserved, got %v", err)
	}
	if !strings.Contains(err.Error(), "4001") {
		t.Fatalf("expected numeric error code to be preserved, got %v", err)
	}
}

func TestDecodeInformationalEventsIgnored(t *testing.T) {
	body := sseBody(
		`event: progress_notice`,
		`data: {"progress":50}`,
		``,
		`event: timing_cost`,
		`data: {"total_ms":1234}`,
		``,
		`event: extra_info`,
		`data: {"info":"some info"}`,
		``,
		`event: metadata`,
		`data: {"model":"test"}`,
		``,
		`event: output`,
		`data: {"response":"ok"}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var text string
	for _, e := range events {
		if e.Delta != "" {
			text += e.Delta
		}
	}
	if text != "ok" {
		t.Errorf("expected text 'ok', got %q", text)
	}
}
