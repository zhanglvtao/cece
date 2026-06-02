package protocol

// Action is the sealed interface for all actions sent from UI to Runtime.
type Action interface{ isAction() }

// InputAction sends user input text to the runtime.
type InputAction struct {
	Text string
}

func (InputAction) isAction() {}

// DryRunRequestAction asks the runtime to build the current model request without sending it.
type DryRunRequestAction struct {
	Input string
}

func (DryRunRequestAction) isAction() {}

// ConfirmAction signals the runtime to proceed with pending tool execution.
type ConfirmAction struct{}

func (ConfirmAction) isAction() {}

// CancelAction signals the runtime to cancel the current operation.
type CancelAction struct{}

func (CancelAction) isAction() {}

// ApprovePlanAction signals the runtime to approve the plan.
type ApprovePlanAction struct{}

func (ApprovePlanAction) isAction() {}

// RejectPlanAction signals the runtime to reject the plan.
type RejectPlanAction struct{}

func (RejectPlanAction) isAction() {}

// AnswerQuestionAction sends user answers back to the runtime.
type AnswerQuestionAction struct {
	Answers []QuestionAnswer
}

func (AnswerQuestionAction) isAction() {}

// SwitchModelAction requests a model switch.
type SwitchModelAction struct {
	Model            string
	MaxContextWindow int
	APIKey           string
	BaseURL          string
	AuthMode         string
	AuthHelper       string
	Protocol         string
	ConfigName       string
}

func (SwitchModelAction) isAction() {}

// CyclePermissionModeAction requests cycling the permission mode.
type CyclePermissionModeAction struct{}

func (CyclePermissionModeAction) isAction() {}

// SetPermissionModeAction requests setting the permission mode directly.
type SetPermissionModeAction struct {
	Mode PermissionMode
}

func (SetPermissionModeAction) isAction() {}

// SetExitTargetModeAction sets the target mode for Exit() to use instead of prePlanMode.
// Must be called before ApprovePlanAction to avoid SetMode racing with Exit().
type SetExitTargetModeAction struct {
	Mode PermissionMode
}

func (SetExitTargetModeAction) isAction() {}

// LoadSessionAction requests loading a session by ID.
type LoadSessionAction struct {
	SessionID string
}

func (LoadSessionAction) isAction() {}

// QueueInputAction queues user input while the agent is busy.
type QueueInputAction struct {
	Text string
}

func (QueueInputAction) isAction() {}

// ListModelsAction requests the runtime to list available models.
type ListModelsAction struct{}

func (ListModelsAction) isAction() {}

// ClearHistoryAction requests the runtime to clear all conversation history.
type ClearHistoryAction struct{}

func (ClearHistoryAction) isAction() {}

// CompactAction requests the runtime to compress conversation history
// by summarizing older messages and preserving recent turns.
type CompactAction struct{}

func (CompactAction) isAction() {}

// TruncateToolResultsAction requests the runtime to truncate all tool_result
// content in conversation history to "[truncated]". Zero API cost, irreversible.
type TruncateToolResultsAction struct{}

func (TruncateToolResultsAction) isAction() {}

// RenameSessionAction requests renaming the current session.
type RenameSessionAction struct {
	SessionID string
	Title     string
}

func (RenameSessionAction) isAction() {}

// AutoTitleSessionAction requests the engine to generate a title for the
// session using a lightweight model call and rename the session accordingly.
// This is a best-effort, fire-and-forget action typically sent on exit.
type AutoTitleSessionAction struct {
	SessionID string
}

func (AutoTitleSessionAction) isAction() {}

// ListMCPAction requests listing all MCP server statuses.
type ListMCPAction struct{}

func (ListMCPAction) isAction() {}

// ConnectMCPAction requests connecting to an MCP server.
type ConnectMCPAction struct {
	Name string
}

func (ConnectMCPAction) isAction() {}

// DisconnectMCPAction requests disconnecting from an MCP server.
type DisconnectMCPAction struct {
	Name string
}

func (DisconnectMCPAction) isAction() {}

// ListToolsAction requests listing all registered tools.
type ListToolsAction struct{}

func (ListToolsAction) isAction() {}
