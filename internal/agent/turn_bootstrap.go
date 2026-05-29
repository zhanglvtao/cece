package agent

import (
	"context"
	"time"

	"cece/internal/prompt"
	"cece/internal/session"
	"cece/internal/tool"
)

type TurnBootstrap struct {
	engine             TurnEngine
	sessionCoordinator *SessionCoordinator
	confirmCh          <-chan struct{}
}

func NewTurnBootstrap(engine TurnEngine, sessionCoordinator *SessionCoordinator, confirmCh <-chan struct{}) *TurnBootstrap {
	return &TurnBootstrap{engine: engine, sessionCoordinator: sessionCoordinator, confirmCh: confirmCh}
}

func (b *TurnBootstrap) Run(ctx context.Context, input string, user Message, snapshot []Message, newSession SessionStartResult, events chan<- Event) {
	if newSession.ID != "" {
		events <- SessionCreated{ID: newSession.ID, Title: newSession.Title}
	}
	events <- UserMessageAdded{Message: user}

	systemPrompt := b.assembleSystemPrompt(input)
	runner := NewTurnRunner(
		b.newModelStreamer(),
		b.newInteractionGate(),
		b.newToolExecutor(),
		b.engine.MaxTokens(),
		b.turnDeps(),
	)
	runner.Run(ctx, TurnRequest{Messages: snapshot, System: systemPrompt}, events)
}

func (b *TurnBootstrap) assembleSystemPrompt(input string) SystemPrompt {
	eng := b.engine
	turnCtx := prompt.TurnContext{
		IncludeTime:             prompt.ShouldInjectTime(input),
		Now:                     time.Now(),
		CurrentWorkingDirectory: eng.ProjectDir(),
		Mode:                    "interactive",
		ConversationTurnNumber:  eng.HistoryLen()/2 + 1,
	}
	if eng.Assembler() == nil {
		return SystemPrompt{}
	}
	return AssembleResultToSystemPrompt(eng.Assembler().Assemble(turnCtx))
}

func (b *TurnBootstrap) newModelStreamer() *ModelStreamer {
	eng := b.engine
	return NewModelStreamer(eng.Client(), eng.Registry(), func(inputTokens int) {
		eng.SetLastInputTokens(inputTokens)
	})
}

func (b *TurnBootstrap) newInteractionGate() *InteractionGate {
	eng := b.engine
	return NewInteractionGate(eng.Registry(), eng.PlanState(), eng.Yolo(), b.confirmCh, func() {
		eng.ResetQuestionAnswers()
	})
}

func (b *TurnBootstrap) newToolExecutor() *ToolExecutor {
	eng := b.engine
	return NewToolExecutor(eng.Registry(), eng.PlanState(), eng.TaskList(), eng.ToolResultPolicy(), func() []tool.QuestionAnswer {
		return append([]tool.QuestionAnswer(nil), eng.GetQuestionAnswers()...)
	})
}

func (b *TurnBootstrap) turnDeps() TurnDeps {
	eng := b.engine
	return TurnDeps{
		AppendMessage: func(msg Message) {
			eng.AppendHistory(msg)
		},
		PersistMessage: eng.PersistMessage,
		UpdateSessionMeta: func(ctx context.Context, resp modelResponse) {
			sessionID, meta, ok := b.updateTokenCounts(resp)
			if !ok {
				return
			}
			b.sessionCoordinator.UpdateMeta(ctx, sessionID, meta)
		},
		DrainQueuedInputs:  eng.DrainQueuedInputs,
		DrainModeReminder:  eng.PlanState().DrainModeReminder,
		HistorySnapshot:    eng.HistorySnapshot,
		IncrementAPICalls:  eng.IncrementAPICalls,
		IncrementToolCount: eng.IncrementToolCount,
		UpdateCacheTokens:  eng.UpdateCacheTokens,
	}
}

func (b *TurnBootstrap) updateTokenCounts(resp modelResponse) (string, session.SessionMeta, bool) {
	return b.engine.IncrementTokens(resp.inputTokens, resp.outputTokens)
}
