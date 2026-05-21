package claude

import (
	"io"
	"strings"
	"testing"
)

func TestParseStreamEmitsDeltasAndDone(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var deltas []string
	var sawDone bool
	for chunk := range chunks {
		if chunk.Delta != "" {
			deltas = append(deltas, chunk.Delta)
		}
		if chunk.Done {
			sawDone = true
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error chunk: %v", chunk.Err)
		}
	}

	if strings.Join(deltas, "") != "Hello" {
		t.Fatalf("joined deltas = %q, want %q", strings.Join(deltas, ""), "Hello")
	}
	if !sawDone {
		t.Fatal("expected Done chunk")
	}
}

func TestParseStreamEmitsErrorChunk(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: error\n" +
		"data: {\"type\":\"error\",\"error\":{\"message\":\"bad request\"}}\n\n"))

	chunks := decodeStreamEvent(body)

	var sawError bool
	for chunk := range chunks {
		if chunk.Err != nil {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected error chunk")
	}
}

func TestParseStreamMessageStartCarriesInputTokens(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":42}}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var gotInputTokens int
	var gotEventType string
	for chunk := range chunks {
		if chunk.EventType == "message_start" {
			gotInputTokens = chunk.InputTokens
			gotEventType = chunk.EventType
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if gotEventType != "message_start" {
		t.Fatalf("EventType = %q, want %q", gotEventType, "message_start")
	}
	if gotInputTokens != 42 {
		t.Fatalf("InputTokens = %d, want %d", gotInputTokens, 42)
	}
}

func TestParseStreamMessageDeltaCarriesOutputTokensAndStopReason(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":7}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var gotOutputTokens int
	var gotStopReason string
	var gotDetail string
	for chunk := range chunks {
		if chunk.EventType == "message_delta" {
			gotOutputTokens = chunk.OutputTokens
			gotStopReason = chunk.StopReason
			gotDetail = chunk.Detail
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if gotOutputTokens != 7 {
		t.Fatalf("OutputTokens = %d, want %d", gotOutputTokens, 7)
	}
	if gotStopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", gotStopReason, "end_turn")
	}
	if gotDetail != "stop_reason" {
		t.Fatalf("Detail = %q, want %q", gotDetail, "stop_reason")
	}
}

func TestParseStreamContentBlockDeltaCarriesEventTypeAndDetail(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var gotEventType string
	var gotDetail string
	for chunk := range chunks {
		if chunk.Delta == "Hel" {
			gotEventType = chunk.EventType
			gotDetail = chunk.Detail
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if gotEventType != "content_block_delta" {
		t.Fatalf("EventType = %q, want %q", gotEventType, "content_block_delta")
	}
	if gotDetail != "text_delta" {
		t.Fatalf("Detail = %q, want %q", gotDetail, "text_delta")
	}
}

func TestParseStreamEmitsToolUseStart(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"Read\",\"input\":{}}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var sawToolUseStart bool
	for chunk := range chunks {
		if chunk.EventType == "content_block_start" && chunk.ToolCallID == "toolu_01" {
			sawToolUseStart = true
			if chunk.ToolCallName != "Read" {
				t.Fatalf("ToolCallName = %q, want %q", chunk.ToolCallName, "Read")
			}
			if chunk.Index != 1 {
				t.Fatalf("Index = %d, want %d", chunk.Index, 1)
			}
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if !sawToolUseStart {
		t.Fatal("expected tool_use content_block_start chunk")
	}
}

func TestParseStreamAssemblesToolCallInput(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"Read\",\"input\":{}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"file\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"_path\\\":\\\"/tmp/test.go\\\"}\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var inputParts []string
	var sawContentBlockStop bool
	for chunk := range chunks {
		if chunk.Detail == "input_json_delta" {
			inputParts = append(inputParts, chunk.ToolCallInput)
			if chunk.Index != 1 {
				t.Fatalf("input_json_delta Index = %d, want 1", chunk.Index)
			}
		}
		if chunk.EventType == "content_block_stop" {
			sawContentBlockStop = true
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}

	joined := strings.Join(inputParts, "")
	if joined != `{"file_path":"/tmp/test.go"}` {
		t.Fatalf("joined input = %q, want %q", joined, `{"file_path":"/tmp/test.go"}`)
	}
	if !sawContentBlockStop {
		t.Fatal("expected content_block_stop chunk")
	}
}

func TestParseStreamStopReasonToolUse(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"Bash\",\"input\":{}}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var stopReason string
	for chunk := range chunks {
		if chunk.EventType == "message_delta" {
			stopReason = chunk.StopReason
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if stopReason != "tool_use" {
		t.Fatalf("StopReason = %q, want %q", stopReason, "tool_use")
	}
}

func TestParseStreamThinkingBlockStart(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var sawThinkingStart bool
	for chunk := range chunks {
		if chunk.EventType == "content_block_start" && chunk.IsThinking {
			sawThinkingStart = true
			if chunk.Index != 0 {
				t.Fatalf("Index = %d, want 0", chunk.Index)
			}
		}
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	if !sawThinkingStart {
		t.Fatal("expected thinking content_block_start chunk with IsThinking=true")
	}
}

func TestParseStreamThinkingDeltaDoesNotLeakToText(t *testing.T) {
	body := io.NopCloser(strings.NewReader("" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"let me think\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"))

	chunks := decodeStreamEvent(body)

	var thinkingDeltas []string
	var textDeltas []string
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.ThinkingDelta != "" {
			thinkingDeltas = append(thinkingDeltas, chunk.ThinkingDelta)
		}
		if chunk.Delta != "" && chunk.Detail != "thinking_delta" {
			textDeltas = append(textDeltas, chunk.Delta)
		}
	}

	if len(thinkingDeltas) != 1 || thinkingDeltas[0] != "let me think" {
		t.Fatalf("thinkingDeltas = %v, want [\"let me think\"]", thinkingDeltas)
	}
	if len(textDeltas) != 1 || textDeltas[0] != "Hello" {
		t.Fatalf("textDeltas = %v, want [\"Hello\"]", textDeltas)
	}
}
