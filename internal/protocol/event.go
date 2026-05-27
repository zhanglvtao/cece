package protocol

import (
	"encoding/json"
	"time"
)

// Event is the sealed interface for all events emitted by the runtime.
type Event interface{ isEvent() }

// SessionCreated is emitted when a new session is auto-created on first input.
type SessionCreated struct {
	ID    string
	Title string
}

func (SessionCreated) isEvent() {}

// UserMessageAdded is emitted when a user message is added to the conversation.
type UserMessageAdded struct{ Message Message }

func (UserMessageAdded) isEvent() {}

// SystemReminderAdded is emitted when a system reminder is injected.
type SystemReminderAdded struct{ Content string }

func (SystemReminderAdded) isEvent() {}

// ModelRequestStarted is emitted before a model API call begins.
type ModelRequestStarted struct {
	Reason               string   // "user" or "tool_result"
	ToolResults          []string // tool result names (when Reason="tool_result")
	EstimatedInputTokens int      // locally estimated input tokens
}

func (ModelRequestStarted) isEvent() {}

// AssistantStarted is emitted when the assistant begins streaming.
type AssistantStarted struct{}

func (AssistantStarted) isEvent() {}

// AssistantDelta is emitted for each text fragment from the assistant.
type AssistantDelta struct{ Text string }

func (AssistantDelta) isEvent() {}

// AssistantCompleted is emitted when the assistant finishes a full response.
type AssistantCompleted struct {
	Duration time.Duration
}

func (AssistantCompleted) isEvent() {}

// RunFailed is emitted when the agent loop encounters an error.
type RunFailed struct{ Err string }

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

// TruncationRetry is emitted when output was truncated and the request is retried.
type TruncationRetry struct {
	Attempt       int
	PrevMaxTokens int
	NewMaxTokens  int
}

func (TruncationRetry) isEvent() {}

// ToolCallStarted is emitted when a tool_use content block begins streaming.
type ToolCallStarted struct {
	ID   string
	Name string
}

func (ToolCallStarted) isEvent() {}

// ToolCallDelta is emitted for incremental tool call input JSON.
type ToolCallDelta struct {
	ID    string
	Delta string
}

func (ToolCallDelta) isEvent() {}

// ToolCallCompleted is emitted when a tool_use content block finishes streaming.
type ToolCallCompleted struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolCallCompleted) isEvent() {}

// ToolCallsReady is emitted when tool calls require user confirmation.
type ToolCallsReady struct {
	Calls []ToolUseBlock
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
	Result ToolResult
}

func (ToolExecCompleted) isEvent() {}

// ThinkingStarted is emitted when a thinking content block begins.
type ThinkingStarted struct {
	Index int // content block index
}

func (ThinkingStarted) isEvent() {}

// ThinkingDelta is emitted for each thinking_delta fragment.
type ThinkingDelta struct{ Text string }

func (ThinkingDelta) isEvent() {}

// ThinkingCompleted is emitted when a thinking content block ends.
type ThinkingCompleted struct {
	Text      string // full assembled thinking text
	Signature string // cryptographic signature for round-trip
}

func (ThinkingCompleted) isEvent() {}

// PlanApprovalRequested is emitted when ExitPlanMode is called and the plan
// is ready for user approval.
type PlanApprovalRequested struct {
	PlanContent string // full markdown content of the plan file
	PlanFile    string // base name of the plan file (e.g. "add-auth.md")
}

func (PlanApprovalRequested) isEvent() {}

// QuestionAsked is emitted when AskUserQuestion tool is called.
type QuestionAsked struct {
	CallID    string
	Questions []Question
}

func (QuestionAsked) isEvent() {}

// QueuedInputPromoted is emitted when a queued input is injected into
// the agent loop between tool calls.
type QueuedInputPromoted struct{}

func (QueuedInputPromoted) isEvent() {}

// TurnCompleted is emitted when a full agent turn finishes (after all
// tool executions and assistant responses). Replaces the old channel-close
// signal so the UI only needs to consume from the single event bus.
type TurnCompleted struct{}

func (TurnCompleted) isEvent() {}

// ── Async query response events ────────────────────────────────────────────

// ModelsLoadedEvent is the response to ListModelsAction.
type ModelsLoadedEvent struct {
	Models []ModelInfo
	Err    string
}

func (ModelsLoadedEvent) isEvent() {}

// ModeChangedEvent is the response to CycleModeAction.
type ModeChangedEvent struct {
	Mode    PermissionMode
	Message string
}

func (ModeChangedEvent) isEvent() {}

// ModeEvent is emitted at startup to report the current permission mode.
type ModeEvent struct {
	Mode PermissionMode
}

func (ModeEvent) isEvent() {}

// HistoryClearedEvent is emitted when conversation history is cleared.
type HistoryClearedEvent struct{}

func (HistoryClearedEvent) isEvent() {}

// SessionLoadedEvent is the response to LoadSessionAction.
type SessionLoadedEvent struct {
	SessionID     string
	History       []Message
	Model         string
	ContextWindow int
	LastInput     int
	TotalInput    int
	TotalOutput   int
	Protocol      string
	ConfigName    string
	Err           string
}

func (SessionLoadedEvent) isEvent() {}
