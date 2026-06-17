package aiden

import (
	"testing"
)

func TestDecodeResponsesTextDelta(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hel"}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"lo"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":10,"output_tokens":2}}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
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

func TestDecodeResponsesThinkingAndText(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"reasoning_summary_text"}}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","output_index":0,"content_index":0,"delta":"let me think"}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"answer"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
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

	if thinkingText != "let me think" {
		t.Errorf("expected thinking 'let me think', got %q", thinkingText)
	}
	if textContent != "answer" {
		t.Errorf("expected text 'answer', got %q", textContent)
	}
	if !thinkingBlockStopSeen {
		t.Error("expected thinking content_block_stop when transitioning to text")
	}
}

func TestDecodeResponsesFunctionCall(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Bash"}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"cmd"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"\":\"ls\"}"}`,
		``,
		`data: {"type":"response.function_call_arguments.done","output_index":1,"arguments":"{\"cmd\":\"ls\"}"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":15,"output_tokens":10}}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolName, toolID string
	var inputParts []string
	var blockStopSeen bool

	for _, e := range events {
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
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

	if toolID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %q", toolID)
	}
	if toolName != "Bash" {
		t.Errorf("expected tool_name 'Bash', got %q", toolName)
	}
	if !blockStopSeen {
		t.Error("expected content_block_stop for function_call")
	}
	combined := ""
	for _, p := range inputParts {
		combined += p
	}
	if combined != `{"cmd":"ls"}` {
		t.Errorf("expected combined input '{\"cmd\":\"ls\"}', got %q", combined)
	}
}

func TestDecodeResponsesError(t *testing.T) {
	body := sseBody(
		`data: {"type":"error","error":{"message":"rate limit exceeded","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err == nil {
		t.Fatal("expected error from error event")
	}
	if err.Error() != "responses api error: rate limit exceeded" {
		t.Errorf("unexpected error message: %v", err)
	}
	_ = events
}

func TestDecodeResponsesUsage(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hi"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"output_tokens":5}}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inputTokens, outputTokens int
	var stopReason string
	for _, e := range events {
		if e.EventType == "message_delta" {
			inputTokens = e.InputTokens
			outputTokens = e.OutputTokens
			stopReason = e.StopReason
		}
	}
	if inputTokens != 100 {
		t.Errorf("expected InputTokens=100, got %d", inputTokens)
	}
	if outputTokens != 5 {
		t.Errorf("expected OutputTokens=5, got %d", outputTokens)
	}
	if stopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", stopReason)
	}
}

func TestDecodeResponsesStopReasonToolUse(t *testing.T) {
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"Bash"}}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{}"}`,
		``,
		`data: {"type":"response.function_call_arguments.done","output_index":1,"arguments":"{}"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":50,"output_tokens":20}}}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stopReason string
	for _, e := range events {
		if e.EventType == "message_delta" {
			stopReason = e.StopReason
		}
	}
	if stopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use' when function_call present, got %q", stopReason)
	}
}

func TestDecodeResponsesDoneSentinel(t *testing.T) {
	body := sseBody(
		`data: [DONE]`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || !events[0].Done {
		t.Error("expected single Done event")
	}
}

func TestDecodeResponsesStreamEndsWithoutDone(t *testing.T) {
	// Stream ends without [DONE] or response.completed — should still emit Done
	body := sseBody(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hi"}`,
		``,
	)
	events, err := collectEvents(DecodeResponsesStream(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doneSeen bool
	for _, e := range events {
		if e.Done {
			doneSeen = true
		}
	}
	if !doneSeen {
		t.Error("expected Done event when stream ends without [DONE]")
	}
}
