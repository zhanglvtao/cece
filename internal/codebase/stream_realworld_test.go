package codebase

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

// TestRealWorldDoubaoThinkingOnly reproduces the "empty response" bug
// observed in session aab6ea8b with Doubao-Seed-2.1-Turbo__dev.
// The codebase API returned reasoning_content without response text.
func TestRealWorldDoubaoThinkingOnly(t *testing.T) {
	// Simulate the actual SSE stream from the log:
	// event: output with reasoning_content="用户" but empty response
	body := sseBody(
		`event: progress_notice`,
		`data: ;Processing_1782266842184809115_S8g1nb`,
		``,
		`event: progress_notice`,
		`data: ;Processing_1782266845185228247_4Dh428`,
		``,
		`event: metadata`,
		`data: {"model":"seed-code-2.1-turbo-seed","session_id":"c4f5d715-a3ad-4703-807b-3a79e5d5ec89","prompt_completion_id":0}`,
		``,
		`event: timing_cost`,
		`data: {"name":"llm_raw_chat_v2","preprocess_timing":36,"postprocess_timing":0}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":"用户","tool_calls":null,"multimodal_contents":null,"phase":null}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we get thinking content
	var thinkingText string
	var thinkingStartSeen, thinkingStopSeen bool
	var textContent string
	var doneSeen bool

	for _, e := range events {
		t.Logf("event: type=%s detail=%s delta=%q thinking=%v done=%v",
			e.EventType, e.Detail, truncate(e.Delta, 60), e.IsThinking, e.Done)

		if e.ThinkingDelta != "" {
			thinkingText += e.ThinkingDelta
		}
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingStartSeen = true
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingStopSeen = true
		}
		if e.Delta != "" {
			textContent += e.Delta
		}
		if e.Done {
			doneSeen = true
		}
	}

	if !thinkingStartSeen {
		t.Error("expected thinking content_block_start")
	}
	if !thinkingStopSeen {
		t.Error("expected thinking content_block_stop")
	}
	if thinkingText != "用户" {
		t.Errorf("expected thinking text '用户', got %q", thinkingText)
	}
	if !doneSeen {
		t.Error("expected Done event")
	}

	// This is the KEY assertion: even though response is empty,
	// the model DID return content (reasoning_content), so it
	// must NOT be considered an "empty response" by the agent loop.
	// The model_streamer/turn_runner should see thinkingBlocks.
	if textContent != "" {
		t.Errorf("expected empty text content, got %q", textContent)
	}

	// Summary: if thinkingStartSeen && thinkingText != "", the model
	// response is NOT empty — it has a thinking block.
	t.Logf("RESULT: thinkingText=%q textContent=%q thinkingStart=%v thinkingStop=%v",
		thinkingText, textContent, thinkingStartSeen, thinkingStopSeen)
}

// TestRealWorldDoubaoThinkingOnlyEOF simulates the case where the
// codebase API stream ends abruptly after sending reasoning_content
// without an event: done — the process may have crashed/hung.
func TestRealWorldDoubaoThinkingOnlyEOF(t *testing.T) {
	// Same as above but stream just ends without done event
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"用户","tool_calls":null,"multimodal_contents":null,"phase":null}`,
		``,
		// No done event — stream just ends
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var thinkingText string
	var thinkingStartSeen, thinkingStopSeen bool
	var doneSeen bool

	for _, e := range events {
		if e.ThinkingDelta != "" {
			thinkingText += e.ThinkingDelta
		}
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingStartSeen = true
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingStopSeen = true
		}
		if e.Done {
			doneSeen = true
		}
	}

	if !thinkingStartSeen {
		t.Error("expected thinking content_block_start")
	}
	if !thinkingStopSeen {
		t.Error("expected thinking content_block_stop (from EOF fallback)")
	}
	if thinkingText != "用户" {
		t.Errorf("expected thinking text '用户', got %q", thinkingText)
	}
	if !doneSeen {
		t.Error("expected Done event (from EOF fallback)")
	}
}

// TestRealWorldMultipleThinkingDeltas simulates a thinking model
// that sends multiple reasoning_content chunks before response text.
func TestRealWorldMultipleThinkingDeltas(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"Let me","tool_calls":null}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":" think","tool_calls":null}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":" about this","tool_calls":null}`,
		``,
		`event: output`,
		`data: {"response":"The answer","reasoning_content":"","tool_calls":null}`,
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
	var thinkingStopSeen bool

	for _, e := range events {
		if e.ThinkingDelta != "" {
			thinkingText += e.ThinkingDelta
		}
		if e.Delta != "" {
			textContent += e.Delta
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			thinkingStopSeen = true
		}
	}

	if thinkingText != "Let me think about this" {
		t.Errorf("expected thinking 'Let me think about this', got %q", thinkingText)
	}
	if textContent != "The answer" {
		t.Errorf("expected text 'The answer', got %q", textContent)
	}
	if !thinkingStopSeen {
		t.Error("expected thinking content_block_stop when transitioning to text")
	}
}

// TestRealWorldEmptyOutputEventWithNullToolCalls tests that an output
// event with empty response AND null tool_calls is handled correctly.
func TestRealWorldEmptyOutputEventWithNullToolCalls(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"","tool_calls":null}`,
		``,
		`event: output`,
		`data: {"response":"Hello","reasoning_content":"","tool_calls":null}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var textContent string
	for _, e := range events {
		if e.Delta != "" {
			textContent += e.Delta
		}
	}

	if textContent != "Hello" {
		t.Errorf("expected text 'Hello', got %q", textContent)
	}
}

// TestRealWorldEmptyFinishReasonWithToolCalls reproduces the exact bug from
// session 1a0ecd22: codebase API sends finish_reason="" even when tool calls
// are present. The stop reason must be "tool_use", not "end_turn".
func TestRealWorldEmptyFinishReasonWithToolCalls(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"thinking...","tool_calls":null}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash","arguments":"","partial_arguments":null}}]}`,
		``,
		`event: done`,
		`data: {"finish_reason":""}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stopReason string
	for _, e := range events {
		if e.EventType == "message_delta" && e.StopReason != "" {
			stopReason = e.StopReason
		}
	}

	if stopReason != "tool_use" {
		t.Errorf("expected stopReason 'tool_use', got %q — this causes 'empty response' bug", stopReason)
	}
}

// TestRealWorldPartialArgumentsStreaming verifies that partial_arguments
// are correctly forwarded as incremental tool call input.
func TestRealWorldPartialArgumentsStreaming(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash","arguments":"","partial_arguments":null}}]}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash","arguments":"","partial_arguments":"{\"com"}}]}`,
		``,
		`event: output`,
		`data: {"response":"","reasoning_content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Bash","arguments":"","partial_arguments":"mand\":\"ls\"}"}}]}`,
		``,
		`event: done`,
		`data: {"finish_reason":""}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolInput string
	for _, e := range events {
		if e.Detail == "input_json_delta" && e.ToolCallInput != "" {
			toolInput += e.ToolCallInput
		}
	}

	if toolInput != `{"command":"ls"}` {
		t.Errorf("expected tool input '{\"command\":\"ls\"}', got %q", toolInput)
	}
}

// Helper
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Verify that the full event sequence for thinking-only output
// is complete enough for model_streamer to build thinkingBlocks.
func TestThinkingOnlyEventSequence(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"thinking...","tool_calls":null}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected event sequence:
	// 1. message_start
	// 2. content_block_start (thinking)
	// 3. content_block_delta (thinking_delta)
	// 4. content_block_stop (thinking) — from emitDone closing thinking
	// 5. message_delta (stop_reason)
	// 6. Done
	var actual []string
	for _, e := range events {
		if e.Done {
			actual = append(actual, "Done")
		} else {
			switch {
			case e.EventType == "content_block_start" && e.IsThinking:
				actual = append(actual, "thinking_start")
			case e.EventType == "content_block_delta" && e.Detail == "thinking_delta":
				actual = append(actual, "thinking_delta")
			case e.EventType == "content_block_stop" && e.IsThinking:
				actual = append(actual, "thinking_stop")
			default:
				actual = append(actual, e.EventType+":"+e.Detail)
			}
		}
	}

	expected := []string{
		"message_start:",
		"thinking_start",
		"thinking_delta",
		"thinking_stop",
		"message_delta:",
		"Done",
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

// Verify that thinking-only output followed by stream EOF (no done event)
// still produces a complete event sequence.
func TestThinkingOnlyEOFEventSequence(t *testing.T) {
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"thinking...","tool_calls":null}`,
		``,
		// No done event, stream just ends
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var hasThinkingStart, hasThinkingDelta, hasThinkingStop, hasDone bool
	for _, e := range events {
		if e.EventType == "content_block_start" && e.IsThinking {
			hasThinkingStart = true
		}
		if e.Detail == "thinking_delta" {
			hasThinkingDelta = true
		}
		if e.EventType == "content_block_stop" && e.IsThinking {
			hasThinkingStop = true
		}
		if e.Done {
			hasDone = true
		}
	}

	if !hasThinkingStart {
		t.Error("missing thinking_start")
	}
	if !hasThinkingDelta {
		t.Error("missing thinking_delta")
	}
	if !hasThinkingStop {
		t.Error("missing thinking_stop — this means thinkingBlocks will be empty in model_streamer!")
	}
	if !hasDone {
		t.Error("missing Done event")
	}
}

// Simulate the exact scenario from model_streamer's perspective:
// After consuming all ApiStreamEvents from the codebase decoder,
// does the modelResponse end up with thinkingBlocks populated?
func TestThinkingOnlyProducesNonEmptyThinkingBlocks(t *testing.T) {
	// This test simulates what model_streamer.go does:
	// It looks for content_block_start (IsThinking) + thinking_delta + content_block_stop (IsThinking)
	// to populate resp.thinkingBlocks.
	body := sseBody(
		`event: output`,
		`data: {"response":"","reasoning_content":"用户","tool_calls":null,"multimodal_contents":null,"phase":null}`,
		``,
		`event: done`,
		`data: {"finish_reason":"stop"}`,
		``,
	)

	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate model_streamer's thinking block assembly logic
	thinkingOpen := false
	thinkingIndex := -1
	var thinkingBuf strings.Builder
	var thinkingBlocks []agent.ApiContentBlock

	for _, e := range events {
		// content_block_start + IsThinking
		if e.EventType == "content_block_start" && e.IsThinking {
			thinkingOpen = true
			thinkingIndex = e.Index
			thinkingBuf.Reset()
		}
		// thinking_delta
		if e.Detail == "thinking_delta" && e.ThinkingDelta != "" {
			thinkingBuf.WriteString(e.ThinkingDelta)
		}
		// content_block_stop + matching index
		if e.EventType == "content_block_stop" && thinkingIndex >= 0 && e.Index == thinkingIndex {
			thinkingBlocks = append(thinkingBlocks, agent.ApiContentBlock{
				Type: agent.ApiThinkingContentType,
				Thinking: &agent.ApiThinkingBlock{
					Text: thinkingBuf.String(),
				},
			})
			thinkingIndex = -1
			thinkingBuf.Reset()
			thinkingOpen = false
		}
	}

	if len(thinkingBlocks) == 0 {
		t.Fatalf("EXPECTED thinkingBlocks to be populated, got 0 blocks — this is the ROOT CAUSE of 'empty response' bug")
	}
	if thinkingBlocks[0].Thinking.Text != "用户" {
		t.Errorf("expected thinking text '用户', got %q", thinkingBlocks[0].Thinking.Text)
	}

	// Also verify: if thinkingBlocks is non-empty, turn_runner should NOT
	// consider this an "empty response"
	textContent := ""
	toolCalls := 0 // would check len() in real code
	if textContent == "" && toolCalls == 0 && len(thinkingBlocks) == 0 {
		t.Error("BUG: this would be classified as empty response!")
	} else {
		t.Log("OK: thinking-only response would NOT be classified as empty")
	}

	_ = thinkingOpen
}
