package chat

import (
	"encoding/json"
	"time"

	"cece/internal/tool"
)

type Event interface{ isEvent() }

// UISessionCreated is emitted when a new session is auto-created on first input.
type UISessionCreated struct {
	ID    string
	Title string
}

func (UISessionCreated) isEvent() {}

type UIUserMessageAdded struct{ Message Message }

func (UIUserMessageAdded) isEvent() {}

type UISystemReminderAdded struct{ Content string }

func (UISystemReminderAdded) isEvent() {}

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
	Attempt       int // retry attempt number (1-based)
	PrevMaxTokens int // previous max_tokens value
	NewMaxTokens  int // new max_tokens value
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
	Text      string // full assembled thinking text
	Signature string // cryptographic signature for round-trip
}

func (UIThinkingCompleted) isEvent() {}

// UIPlanApprovalRequested is emitted when ExitPlanMode is called and the plan
// is ready for user approval. The UI should render the plan content and show
// an approval dialog.
type UIPlanApprovalRequested struct {
	PlanContent string // full markdown content of the plan file
	PlanFile    string // base name of the plan file (e.g. "add-auth.md")
}

func (UIPlanApprovalRequested) isEvent() {}

// UIQuestionAsked is emitted when AskUserQuestion tool is called. The UI
// should render a question dialog, collect user answers, then call
// Runtime.AnswerQuestion() to continue the agent loop.
type UIQuestionAsked struct {
	CallID    string
	Questions []tool.Question
}

func (UIQuestionAsked) isEvent() {}

// UIQueuedInputPromoted is emitted when a queued input is injected into
// the agent loop between tool calls. The UI should promote the next faint
// queued item to normal styling.
type UIQueuedInputPromoted struct{}

func (UIQueuedInputPromoted) isEvent() {}

// UICompacting is emitted when compaction starts.
type UICompacting struct{}

func (UICompacting) isEvent() {}

// UICompacted is emitted when conversation history has been compressed.
type UICompacted struct {
	TokensBefore   int
	TokensAfter    int
	MessagesBefore int
	MessagesAfter  int
}

func (UICompacted) isEvent() {}

// UITurnCompleted is emitted when a full agent turn finishes.
type UITurnCompleted struct{}

func (UITurnCompleted) isEvent() {}
