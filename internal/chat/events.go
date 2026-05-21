package chat

import (
	"encoding/json"
	"time"

	"cece/internal/tool"
)

type Event interface{ isEvent() }

type UIUserMessageAdded struct{ Message Message }

func (UIUserMessageAdded) isEvent() {}

type UIModelRequestStarted struct {
	Reason               string   // "user" or "tool_result"
	ToolResults          []string // tool result names (when Reason="tool_result")
	EstimatedInputTokens int      // locally estimated input tokens (system + messages + tools)
}

func (UIModelRequestStarted) isEvent() {}

type UIAssistantStarted struct{}

func (UIAssistantStarted) isEvent() {}

type UIAssistantDelta struct{ Text string }

func (UIAssistantDelta) isEvent() {}

type UIAssistantCompleted struct {
	Duration time.Duration
}

func (UIAssistantCompleted) isEvent() {}

type UIRunFailed struct{ Err error }

func (UIRunFailed) isEvent() {}

// UIStreamStarted is emitted when the SSE stream opens.
type UIStreamStarted struct {
	Model               string
	InputTokens         int
	Tools               []string // tool names registered for this request
	CacheCreationTokens int
	CacheReadTokens     int
}

func (UIStreamStarted) isEvent() {}

// UIStreamEventDetail is emitted for each raw SSE event (for UI debug display).
type UIStreamEventDetail struct {
	EventType string // "content_block_delta", "message_delta", etc.
	Detail    string // "text_delta", "stop_reason", etc.
	Text      string // delta text, truncated to 60 chars
}

func (UIStreamEventDetail) isEvent() {}

// UIStreamCompleted is emitted when the SSE stream closes successfully.
type UIStreamCompleted struct {
	OutputTokens int
	StopReason   string
	Duration     time.Duration
	ToolCalls    []string // tool names requested in this response
}

func (UIStreamCompleted) isEvent() {}

// UITruncationRetry is emitted when the output was truncated and we're retrying with larger max_tokens.
type UITruncationRetry struct {
	Attempt      int   // retry attempt number (1-based)
	PrevMaxTokens int  // previous max_tokens value
	NewMaxTokens  int  // new max_tokens value
}

func (UITruncationRetry) isEvent() {}

// UIToolCallStarted is emitted when a tool_use content block begins.
type UIToolCallStarted struct {
	ID    string
	Name  string
	Index int
}

func (UIToolCallStarted) isEvent() {}

// UIToolCallDelta is emitted for each input_json_delta fragment.
type UIToolCallDelta struct {
	ID    string
	Index int
	Input string
}

func (UIToolCallDelta) isEvent() {}

// UIToolCallCompleted is emitted when a tool_use content block ends
// and the full input JSON has been assembled.
type UIToolCallCompleted struct {
	ID    string
	Name  string
	Input json.RawMessage
	Index int
}

func (UIToolCallCompleted) isEvent() {}

// UIToolCallsReady is emitted when stop_reason is "tool_use",
// notifying the UI that tool execution needs user confirmation.
type UIToolCallsReady struct {
	Calls []ApiToolUseBlock
}

func (UIToolCallsReady) isEvent() {}

// UIToolExecStarted is emitted when a tool begins executing.
type UIToolExecStarted struct {
	ID   string
	Name string
}

func (UIToolExecStarted) isEvent() {}

// UIToolExecDelta is emitted for each line of streaming tool output.
type UIToolExecDelta struct {
	ID   string
	Text string
}

func (UIToolExecDelta) isEvent() {}

// UIToolExecCompleted is emitted when a tool finishes executing.
type UIToolExecCompleted struct {
	ID     string
	Name   string
	Result tool.Result
}

func (UIToolExecCompleted) isEvent() {}

// UIThinkingStarted is emitted when a thinking content block begins.
type UIThinkingStarted struct {
	Index int // content block index
}

func (UIThinkingStarted) isEvent() {}

// UIThinkingDelta is emitted for each thinking_delta fragment.
type UIThinkingDelta struct {
	Text string
}

func (UIThinkingDelta) isEvent() {}

// UIThinkingCompleted is emitted when a thinking content block ends.
type UIThinkingCompleted struct {
	Text string // full assembled thinking text
}

func (UIThinkingCompleted) isEvent() {}
