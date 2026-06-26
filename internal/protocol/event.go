package protocol

import (
	"encoding/json"
	"time"
)

// Event is the sealed interface for all events emitted by the runtime.
type Event interface{ isEvent() }

// ObservatoryServerStartedEvent is emitted after the local observability
// HTTP server binds to a concrete port.
type ObservatoryServerStartedEvent struct {
	URL  string
	Host string
	Port int
}

func (ObservatoryServerStartedEvent) isEvent() {}

// ObservatorySnapshotEvent carries a full observability snapshot for one scope.
type ObservatorySnapshotEvent struct {
	Scope       string
	Version     int
	CapturedAt  time.Time
	ActivePhase string
	Nodes       []ObservatoryNode
	Edges       []ObservatoryEdge
	Phases      []ObservatoryPhase
	Metrics     []ObservatoryMetric
	Evidence    []string
}

func (ObservatorySnapshotEvent) isEvent() {}

// EngineReadyEvent is emitted once when the engine process starts,
// carrying initial model info so the TUI can sync its state.
type EngineReadyEvent struct {
	Model         string
	ContextWindow int
	Effort        string
}

func (EngineReadyEvent) isEvent() {}

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
	APICalls             int      // cumulative API call count (injected by Engine)
	ContextWindow        int      // engine's current context window size (synced to TUI)
}

func (ModelRequestStarted) isEvent() {}

type PromptLayerDryRun struct {
	Name          string
	CacheControl  map[string]string
	TokenEstimate int
	Content       string
}

type MessageDryRun struct {
	Index   int
	Role    string
	Content string
}

type ToolDryRun struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// RequestDryRunEvent contains the full request preview built without a model call.
type RequestDryRunEvent struct {
	Input                string
	MaxTokens            int
	EstimatedInputTokens int
	PromptLayers         []PromptLayerDryRun
	Messages             []MessageDryRun
	Tools                []ToolDryRun
}

func (RequestDryRunEvent) isEvent() {}

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

type CompletionGateStatus string

const (
	CompletionGatePassed  CompletionGateStatus = "passed"
	CompletionGateBlocked CompletionGateStatus = "blocked"
	CompletionGateSkipped CompletionGateStatus = "skipped"
)

type CompletionGateCheck struct {
	Name    string
	Status  CompletionGateStatus
	Summary string
	Details []string
}

type CompletionGateEvaluated struct {
	Attempt         int
	MaxAttempts     int
	Status          CompletionGateStatus
	RequiresClosure bool
	Checks          []CompletionGateCheck
	Next            string
}

func (CompletionGateEvaluated) isEvent() {}

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
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	StopReason      string
	Duration        time.Duration
	ToolCalls       []string // tool names requested in this response
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
	ID         string
	Name       string
	Result     ToolResult
	ToolCounts map[string]int // cumulative tool counts (injected by Engine)
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

// PlanRejected is emitted when the user rejects a plan approval request.
type PlanRejected struct{}

func (PlanRejected) isEvent() {}

// ToolCallsRejected is emitted when the user rejects tool call confirmation.
type ToolCallsRejected struct{}

func (ToolCallsRejected) isEvent() {}

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

// CompactingEvent is emitted when compaction starts.
type CompactingEvent struct{}

func (CompactingEvent) isEvent() {}

// CompactedEvent is emitted when conversation history has been compressed.
type CompactedEvent struct {
	TokensBefore   int // estimated input tokens before compaction
	TokensAfter    int // estimated input tokens after compaction
	MessagesBefore int
	MessagesAfter  int
	Summary        string // the generated summary content
	Err            string // non-empty when compaction failed
}

func (CompactedEvent) isEvent() {}

// TruncatedToolResultsEvent is emitted after tool_result contents are truncated.
type TruncatedToolResultsEvent struct {
	TruncatedCount int // number of tool_result blocks truncated
	TokensBefore   int // estimated tokens before truncation
	TokensAfter    int // estimated tokens after truncation
}

func (TruncatedToolResultsEvent) isEvent() {}

// PrunedEvent is emitted when messages before a turn are pruned entirely.
type PrunedEvent struct {
	TokensBefore   int
	TokensAfter    int
	MessagesBefore int
	MessagesAfter  int
	PrunedTurns    int
}

func (PrunedEvent) isEvent() {}

// ContextNudgedEvent is emitted when the system injects a context-pressure
// reminder into the conversation to nudge the LLM toward compacting.
type ContextNudgedEvent struct {
	TurnsSinceCompact int
	ContextPct        int // 0-100
	ContextUsed       int // tokens
	ContextWindow     int // tokens
}

func (ContextNudgedEvent) isEvent() {}

// TurnCompleted is emitted when a full agent turn finishes (after all
// tool executions and assistant responses). Replaces the old channel-close
// signal so the UI only needs to consume from the single event bus.
type TurnCompleted struct {
	LastInputTokens     int // input tokens of the last API request (= current context usage)
	TotalInputTokens    int // cumulative input tokens across all API calls in this turn
	TotalOutputTokens   int // cumulative output tokens across all API calls in this turn
	CacheReadTokens     int // cumulative cache read tokens across this session
	CacheCreationTokens int // cumulative cache creation tokens across this session
	TurnCount           int // cumulative conversation turn count
	ContextWindow       int // engine's current context window size (synced to TUI)
}

func (TurnCompleted) isEvent() {}

// SessionTitleGeneratedEvent is emitted when an async title generation completes.
type SessionTitleGeneratedEvent struct {
	SessionID string
	Title     string // generated title (empty on error)
	Err       string // error message (empty on success)
}

func (SessionTitleGeneratedEvent) isEvent() {}

// SessionDeletedEvent is emitted after a session has been deleted.
type SessionDeletedEvent struct {
	SessionID string
}

func (SessionDeletedEvent) isEvent() {}

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

// EffortChangedEvent is the response to SetEffortAction.
type EffortChangedEvent struct {
	Effort string
}

func (EffortChangedEvent) isEvent() {}

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
	SessionID           string
	History             []Message
	Model               string
	ContextWindow       int
	LastInput           int
	TotalInput          int
	TotalOutput         int
	Protocol            string
	ConfigName          string
	APICalls            int
	ToolCounts          map[string]int
	CacheReadTokens     int
	CacheCreationTokens int
	TurnCount           int
	CompletionHookCalls int
	InputHistory        []string
	Err                 string
}

func (SessionLoadedEvent) isEvent() {}

// MCPServersListedEvent is the response to ListMCPAction.
type MCPServersListedEvent struct {
	Servers []MCPServerInfo
}

func (MCPServersListedEvent) isEvent() {}

// MCPServerInfo describes a single MCP server for the UI.
type MCPServerInfo struct {
	Name      string
	Type      string
	Addr      string
	Connected bool
	ToolCount int
	Error     string
}

// MCPServerStatusChangedEvent is emitted after connect/disconnect.
type MCPServerStatusChangedEvent struct {
	Name      string
	Connected bool
	Error     string
}

func (MCPServerStatusChangedEvent) isEvent() {}

// TodoUpdatedEvent is emitted when the task list changes.
type TodoUpdatedEvent struct {
	Tasks []TodoItem
}

func (TodoUpdatedEvent) isEvent() {}

type AgentBusEvent struct {
	MessageID       string         `json:"message_id"`
	TraceID         string         `json:"trace_id,omitempty"`
	CausationID     string         `json:"causation_id,omitempty"`
	AgentID         string         `json:"agent_id"`
	ParentSessionID string         `json:"parent_session_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	Kind            string         `json:"kind"`
	StatusFrom      string         `json:"status_from,omitempty"`
	StatusTo        string         `json:"status_to,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
}

func (AgentBusEvent) isEvent() {}

// SubAgentStartedEvent is emitted when a sub-agent begins executing.
type SubAgentStartedEvent struct {
	ID              string
	Description     string
	SessionID       string
	ParentSessionID string
}

func (SubAgentStartedEvent) isEvent() {}

// SubAgentActivityEvent is emitted when a running sub-agent reports current activity.
type SubAgentActivityEvent struct {
	ID              string
	SessionID       string
	ParentSessionID string
	Activity        string
	Status          string
	// Structured fields for the agent bar view — filled by forwardSubAgentActivity.
	Model            string // model name from StreamStarted
	InputTokens      int    // cumulative input tokens
	OutputTokens     int    // cumulative output tokens
	CacheReadTokens  int    // cumulative cache read tokens
	TurnCount        int    // number of LLM turns completed so far
	ToolCall         string // formatted current tool call, e.g. "Bash command: \"find...\""
	LastAssistantMsg string // most recent assistant text snippet (first line)
}

func (SubAgentActivityEvent) isEvent() {}

// SubAgentCompletedEvent is emitted when a sub-agent finishes successfully.
type SubAgentCompletedEvent struct {
	ID              string
	Description     string
	SessionID       string
	ParentSessionID string
	InputTokens     int
	OutputTokens    int
	TurnsUsed       int
	HitMaxTurns     bool
}

func (SubAgentCompletedEvent) isEvent() {}

// SubAgentFailedEvent is emitted when a sub-agent fails.
type SubAgentFailedEvent struct {
	ID              string
	Description     string
	SessionID       string
	ParentSessionID string
	Error           string
}

func (SubAgentFailedEvent) isEvent() {}

// ToolsListedEvent is the response to ListToolsAction.
type ToolsListedEvent struct {
	Tools []ToolInfo
}

func (ToolsListedEvent) isEvent() {}

// ToolInfo describes a single tool for the UI.
type ToolInfo struct {
	Name        string
	Description string
	Source      string // "builtin" or "mcp:<server>"
}

// StatsEvent is the response to StatsAction, carrying cumulative session statistics.
type StatsEvent struct {
	Stats SessionStats
}

func (StatsEvent) isEvent() {}
