package agent

import (
	"encoding/json"
	"time"

	"cece/internal/tool"
)

type Event interface{ isEvent() }

// SessionCreated is emitted when a new session is auto-created on first input.
type SessionCreated struct {
	ID    string
	Title string
}

func (SessionCreated) isEvent() {}

type UserMessageAdded struct{ Message Message }

func (UserMessageAdded) isEvent() {}

type SystemReminderAdded struct{ Content string }

func (SystemReminderAdded) isEvent() {}

type ModelRequestStarted struct {
	Reason               string   // "user" or "tool_result"
	ToolResults          []string // tool result names (when Reason="tool_result")
	EstimatedInputTokens int      // locally estimated input tokens (system + messages + tools)
}

func (ModelRequestStarted) isEvent() {}

type AssistantStarted struct{}

func (AssistantStarted) isEvent() {}

type AssistantDelta struct{ Text string }

func (AssistantDelta) isEvent() {}

type AssistantCompleted struct {
	Duration time.Duration
}

func (AssistantCompleted) isEvent() {}

type RunFailed struct{ Err error }

func (RunFailed) isEvent() {}

// StreamStarted is emitted when the SSE stream opens.
type StreamStarted struct {
	Model               string
	InputTokens         int
	Tools               []string // tool names registered for this request
	CacheCreationTokens int
	CacheReadTokens     int
}

func (StreamStarted) isEvent() {}

// StreamEventDetail is emitted for each raw SSE event (for UI debug display).
type StreamEventDetail struct {
	EventType string // "content_block_delta", "message_delta", etc.
	Detail    string // "text_delta", "stop_reason", etc.
	Text      string // delta text, truncated to 60 chars
}

func (StreamEventDetail) isEvent() {}

// StreamCompleted is emitted when the SSE stream closes successfully.
type StreamCompleted struct {
	OutputTokens int
	StopReason   string
	Duration     time.Duration
	ToolCalls    []string // tool names requested in this response
}

func (StreamCompleted) isEvent() {}

// TruncationRetry is emitted when the output was truncated and we're retrying with larger max_tokens.
type TruncationRetry struct {
	Attempt       int // retry attempt number (1-based)
	PrevMaxTokens int // previous max_tokens value
	NewMaxTokens  int // new max_tokens value
}

func (TruncationRetry) isEvent() {}

// ToolCallStarted is emitted when a tool_use content block begins.
type ToolCallStarted struct {
	ID    string
	Name  string
	Index int
}

func (ToolCallStarted) isEvent() {}

// ToolCallDelta is emitted for each input_json_delta fragment.
type ToolCallDelta struct {
	ID    string
	Index int
	Input string
}

func (ToolCallDelta) isEvent() {}

// ToolCallCompleted is emitted when a tool_use content block ends
// and the full input JSON has been assembled.
type ToolCallCompleted struct {
	ID    string
	Name  string
	Input json.RawMessage
	Index int
}

func (ToolCallCompleted) isEvent() {}

// ToolCallsReady is emitted when stop_reason is "tool_use",
// notifying the UI that tool execution needs user confirmation.
type ToolCallsReady struct {
	Calls []ApiToolUseBlock
}

func (ToolCallsReady) isEvent() {}

// ToolExecStarted is emitted when a tool begins executing.
type ToolExecStarted struct {
	ID   string
	Name string
}

func (ToolExecStarted) isEvent() {}

// ToolExecDelta is emitted for each line of streaming tool output.
type ToolExecDelta struct {
	ID   string
	Text string
}

func (ToolExecDelta) isEvent() {}

// ToolExecCompleted is emitted when a tool finishes executing.
type ToolExecCompleted struct {
	ID     string
	Name   string
	Result tool.Result
}

func (ToolExecCompleted) isEvent() {}

// ThinkingStarted is emitted when a thinking content block begins.
type ThinkingStarted struct {
	Index int // content block index
}

func (ThinkingStarted) isEvent() {}

// ThinkingDelta is emitted for each thinking_delta fragment.
type ThinkingDelta struct {
	Text string
}

func (ThinkingDelta) isEvent() {}

// ThinkingCompleted is emitted when a thinking content block ends.
type ThinkingCompleted struct {
	Text      string // full assembled thinking text
	Signature string // cryptographic signature for round-trip
}

func (ThinkingCompleted) isEvent() {}

// PlanApprovalRequested is emitted when ExitPlanMode is called and the plan
// is ready for user approval. The UI should render the plan content and show
// an approval dialog.
type PlanApprovalRequested struct {
	PlanContent string // full markdown content of the plan file
	PlanFile    string // base name of the plan file (e.g. "add-auth.md")
}

func (PlanApprovalRequested) isEvent() {}

// QuestionAsked is emitted when AskUserQuestion tool is called. The UI
// should render a question dialog, collect user answers, then call
// Runtime.AnswerQuestion() to continue the agent loop.
type QuestionAsked struct {
	CallID    string
	Questions []tool.Question
}

func (QuestionAsked) isEvent() {}

// QueuedInputPromoted is emitted when a queued input is injected into
// the agent loop between tool calls. The UI should promote the next faint
// queued item to normal styling.
type QueuedInputPromoted struct{}

func (QueuedInputPromoted) isEvent() {}

// Compacting is emitted when compaction starts.
type Compacting struct{}

func (Compacting) isEvent() {}

// Compacted is emitted when conversation history has been compressed.
type Compacted struct {
	TokensBefore   int
	TokensAfter    int
	MessagesBefore int
	MessagesAfter  int
	Summary        string
}

func (Compacted) isEvent() {}

// TurnCompleted is emitted when a full agent turn finishes.
type TurnCompleted struct{}

func (TurnCompleted) isEvent() {}

// TaskUpdated is emitted when the task list changes.
type TaskUpdated struct {
	Tasks []tool.TaskItem
}

func (TaskUpdated) isEvent() {}
