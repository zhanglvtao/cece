# Codebase Provider Protocol Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the "model returned empty response 3 consecutive times" issue by properly accumulating fragmented Server-Sent Events (SSE) tool call deltas in the stream decoder.

**Architecture:** We will introduce a stateful `toolCallAccumulator` in `internal/codebase/stream.go` to collect `id` and `name` fields for tool calls across multiple SSE chunks before emitting the `content_block_start` event. We will also add fallback parsing for empty event types. Serialization logic remains strictly unchanged to avoid proxy validation errors.

**Tech Stack:** Go, standard library `testing`, SSE parsing.

---

### Task 1: Add Tool Call Accumulator and Fallback Event Parsing

**Files:**
- Modify: `internal/codebase/stream.go`

- [ ] **Step 1: Write the failing test**

Create a new file `internal/codebase/stream_fragmented_test.go`:

```go
package codebase

import (
	"strings"
	"testing"
)

func TestDecodeToolCallFragmentedStart(t *testing.T) {
	body := sseBody(
		`data: {"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{}}]}`,
		``,
		`data: {"tool_calls":[{"index":0,"type":"function","function":{"name":"Bash"}}]}`,
		``,
		`data: {"tool_calls":[{"index":0,"type":"function","function":{"arguments":"{\"cmd\""}}]}`,
		``,
		`data: {"tool_calls":[{"index":0,"type":"function","function":{"arguments":":\"ls\"}"}}]}`,
		``,
		`data: {"finish_reason":"tool_calls"}`,
		``,
	)
	events, err := collectEvents(DecodeStreamEvent(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var startCount int
	var toolName, toolID string
	var inputParts []string

	for _, e := range events {
		if e.EventType == "content_block_start" && e.ToolCallID != "" {
			startCount++
			toolID = e.ToolCallID
			toolName = e.ToolCallName
		}
		if e.Detail == "input_json_delta" {
			inputParts = append(inputParts, e.ToolCallInput)
		}
	}

	if startCount != 1 {
		t.Errorf("expected exactly 1 start event, got %d", startCount)
	}
	if toolID != "call_1" {
		t.Errorf("expected tool ID 'call_1', got %q", toolID)
	}
	if toolName != "Bash" {
		t.Errorf("expected tool name 'Bash', got %q", toolName)
	}
	expectedInput := `{"cmd":"ls"}`
	actualInput := strings.Join(inputParts, "")
	if actualInput != expectedInput {
		t.Errorf("expected input %q, got %q", expectedInput, actualInput)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestDecodeToolCallFragmentedStart ./internal/codebase/...`
Expected: FAIL with `expected exactly 1 start event, got 0`

- [ ] **Step 3: Write minimal implementation**

Modify `internal/codebase/stream.go`:

Add `toolCallAccumulator` and update `streamState`:

```go
// ... existing code ...
type ErrorEvent struct {
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
	Code    int    `json:"code,omitempty"`
}

type toolCallAccumulator struct {
	ID      string
	Name    string
	Emitted bool
}

type streamState struct {
	messageStarted    bool
	thinkingOpen      bool
	thinkingIndex     int
	activeToolIndices map[int]bool
	textBlockStarted  bool
	textBlockIndex    int
	inputTokens       int
	outputTokens      int
	doneEmitted       bool
	toolCalls         map[int]*toolCallAccumulator
}
// ... existing code ...
```

Update `processEvent` for fallback parsing:

```go
func processEvent(eventType, data string, out chan<- agent.ApiStreamEvent, state *streamState) {
	if eventType == "" {
		// Try to parse as output event if there is no event type
		var ev OutputEvent
		if err := json.Unmarshal([]byte(data), &ev); err == nil {
			if ev.Response != "" || ev.ReasoningContent != "" || len(ev.ToolCalls) > 0 {
				emitOutput(&ev, out, state)
				return
			}
		}
	}

	switch eventType {
// ... existing code ...
```

Update `emitOutput` to use the accumulator:

```go
// ... inside emitOutput ...
	// Tool calls
	for _, tc := range ev.ToolCalls {
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
		}
		if state.toolCalls == nil {
			state.toolCalls = make(map[int]*toolCallAccumulator)
		}

		acc, ok := state.toolCalls[tc.Index]
		if !ok {
			acc = &toolCallAccumulator{}
			state.toolCalls[tc.Index] = acc
		}

		if tc.ID != "" {
			acc.ID = tc.ID
		}

		fn := tc.effectiveFunctionCall()
		if fn != nil && fn.Name != "" {
			acc.Name = fn.Name
		}

		// Emit start event once we have both ID and Name (or just Name if it's the only thing we get)
		if !acc.Emitted && acc.Name != "" && acc.ID != "" {
			acc.Emitted = true
			state.activeToolIndices[tc.Index] = true
			out <- agent.ApiStreamEvent{
				EventType:    "content_block_start",
				ToolCallID:   acc.ID,
				ToolCallName: acc.Name,
				Index:        tc.Index,
			}
		}

		// Subsequent: input_json_delta (always process if arguments present)
		if fn != nil && fn.Arguments != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: fn.Arguments,
				Index:         tc.Index,
			}
		}
	}
// ... existing code ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestDecodeToolCallFragmentedStart ./internal/codebase/...`
Expected: PASS

- [ ] **Step 5: Run all codebase tests**

Run: `go test ./internal/codebase/...`
Expected: PASS (no regressions)

- [ ] **Step 6: Commit**

```bash
git add internal/codebase/stream.go internal/codebase/stream_fragmented_test.go
git commit -m "fix(codebase): properly accumulate fragmented tool call SSE chunks"
```