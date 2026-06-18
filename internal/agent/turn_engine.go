package agent

import (
	"context"

	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type UsageRecord struct {
	SessionID                string
	Model                    string
	InputTokens              int
	OutputTokens             int
	CacheReadTokens          int
	CacheCreationTokens      int
	TotalInputTokens         int
	TotalOutputTokens        int
	TotalCacheReadTokens     int
	TotalCacheCreationTokens int
}

// TurnEngine is the interface that TurnBootstrap needs from the agent engine.
// Engine (in internal/engine) implements this interface.
type TurnEngine interface {
	// Config
	ProjectDir() string
	Assembler() *prompt.ContextAssembler
	Client() ModelClient
	Registry() *tool.Registry
	PlanState() *tool.PlanModeState
	TaskList() *tool.TaskList
	Yolo() bool
	MaxTokens() int
	ContextWindow() int
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
	RecordUsage(ctx context.Context, usage UsageRecord)

	// Status bar tracking (Engine is the single source of truth)
	IncrementAPICalls()
	RecordToolExecution(name string, isError bool)
	UpdateCacheTokens(read, creation int)

	// Question answers
	ResetQuestionAnswers()
	GetQuestionAnswers() []tool.QuestionAnswer

	// Queued inputs
	DrainQueuedInputs() []string

	// Auto compact
	TryAutoCompact(ctx context.Context) bool
}
