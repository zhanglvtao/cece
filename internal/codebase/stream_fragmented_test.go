package codebase

import (
	"bytes"
	"io"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

func TestStreamFragmentedToolCall(t *testing.T) {
	// Simulate SSE stream where tool call ID and Name arrive in different chunks.
	streamData := `event: output
data: {"response":"", "tool_calls":[{"index":0, "id":"call_abc", "type":"function"}]}

event: output
data: {"response":"", "tool_calls":[{"index":0, "function":{"name":"Bash"}}]}

event: output
data: {"response":"", "tool_calls":[{"index":0, "function":{"arguments":"{\"command\":\"ls\"}"}}]}

event: done
data: {"finish_reason":"tool_calls"}

`
	body := io.NopCloser(bytes.NewReader([]byte(streamData)))
	ch := DecodeStreamEvent(body)

	events, err := collectEvents(ch)
	if err != nil {
		t.Fatalf("collectEvents failed: %v", err)
	}

	var startEvents []agent.ApiStreamEvent
	var deltaEvents []agent.ApiStreamEvent
	for _, ev := range events {
		if ev.EventType == "content_block_start" && ev.ToolCallID != "" {
			startEvents = append(startEvents, ev)
		}
		if ev.EventType == "content_block_delta" && ev.Detail == "input_json_delta" {
			deltaEvents = append(deltaEvents, ev)
		}
	}

	if len(startEvents) != 1 {
		t.Fatalf("expected 1 tool call start event, got %d", len(startEvents))
	}
	if startEvents[0].ToolCallID != "call_abc" {
		t.Errorf("expected tool call ID 'call_abc', got %q", startEvents[0].ToolCallID)
	}
	if startEvents[0].ToolCallName != "Bash" {
		t.Errorf("expected tool call name 'Bash', got %q", startEvents[0].ToolCallName)
	}

	if len(deltaEvents) == 0 {
		t.Fatal("expected at least 1 input_json_delta event")
	}
	
	// Reconstruct the full arguments
	var args string
	for _, ev := range deltaEvents {
		args += ev.ToolCallInput
	}
	if args != `{"command":"ls"}` {
		t.Errorf("expected arguments '{\"command\":\"ls\"}', got %q", args)
	}
}
