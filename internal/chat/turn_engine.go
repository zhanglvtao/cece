package chat

import (
	"context"

	"cece/internal/prompt"
	"cece/internal/session"
	"cece/internal/tool"
)

// TurnEngine is the interface that TurnBootstrap needs from the agent engine.
// Engine (in internal/engine) implements this interface.
type TurnEngine interface {
	// Config
	ProjectDir() string
	Assembler() *prompt.ContextAssembler
	Client() ModelClient
	Registry() *tool.Registry
	PlanState() *tool.PlanModeState
	Yolo() bool
	MaxTokens() int
	ToolResultPolicy() ToolResultPolicy
	SessionID() string

	// History
	HistoryLen() int
	AppendHistory(Message)
	PersistMessage(ctx context.Context, msg Message)
	HistorySnapshot() []Message

	// Token tracking
	SetLastInputTokens(int)
	IncrementTokens(input, output int) (sessionID string, meta session.SessionMeta, ok bool)

	// Question answers
	ResetQuestionAnswers()
	GetQuestionAnswers() []tool.QuestionAnswer

	// Queued inputs
	DrainQueuedInputs() []string
}
