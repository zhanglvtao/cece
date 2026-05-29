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

	req := b.BuildTurnRequest(input, snapshot)
	runner := NewTurnRunner(
		b.newModelStreamer(),
		b.newInteractionGate(),
		b.newToolExecutor(),
		b.engine.MaxTokens(),
		b.turnDeps(),
	)
	runner.Run(ctx, req, events)
}

func (b *TurnBootstrap) BuildTurnRequest(input string, snapshot []Message) TurnRequest {
	return TurnRequest{Messages: snapshot, System: b.assembleSystemPrompt(input)}
}

func (b *TurnBootstrap) BuildDryRunRequest(input string, snapshot []Message) RequestDryRun {
	eng := b.engine
	assembleResult := prompt.AssembleResult{}
	if eng.Assembler() != nil {
		turnCtx := prompt.TurnContext{
			IncludeTime:             prompt.ShouldInjectTime(input),
			Now:                     time.Now(),
			CurrentWorkingDirectory: eng.ProjectDir(),
			Mode:                    "interactive",
			ConversationTurnNumber:  eng.HistoryLen()/2 + 1,
		}
		assembleResult = eng.Assembler().Assemble(turnCtx)
	}

	systemPrompt := AssembleResultToSystemPrompt(assembleResult)
	tools := toolDefinitions(eng.Registry())
	return RequestDryRun{
		Input:               input,
		MaxTokens:           eng.MaxTokens(),
		EstimatedInputTokens: EstimateRequestTokens(systemPrompt, snapshot, tools),
		PromptLayers:        promptLayerDryRuns(assembleResult),
		SystemBlocks:        systemBlockDryRuns(systemPrompt),
		Messages:            messageDryRuns(snapshot),
		Tools:               toolDryRuns(tools),
	}
}

func toolDefinitions(reg *tool.Registry) []tool.Definition {
	if reg == nil {
		return nil
	}
	return reg.Definitions()
}

func promptLayerDryRuns(r prompt.AssembleResult) []PromptLayerDryRun {
	out := make([]PromptLayerDryRun, 0, len(r.Segments))
	for _, seg := range r.Segments {
		out = append(out, PromptLayerDryRun{
			Name:          seg.Layer.String(),
			CacheControl:  seg.Layer.CacheControl(),
			TokenEstimate: prompt.EstimateTokens(seg.Content),
			Content:       seg.Content,
		})
	}
	return out
}

func systemBlockDryRuns(system SystemPrompt) []SystemBlockDryRun {
	out := make([]SystemBlockDryRun, 0, len(system.Blocks))
	for i, block := range system.Blocks {
		out = append(out, SystemBlockDryRun{
			Index:         i,
			CacheControl:  block.CacheControl,
			TokenEstimate: prompt.EstimateTokens(block.Text),
			Text:          block.Text,
		})
	}
	return out
}

func messageDryRuns(messages []Message) []MessageDryRun {
	out := make([]MessageDryRun, 0, len(messages))
	for i, msg := range messages {
		out = append(out, MessageDryRun{
			Index:   i,
			Role:    string(msg.Role),
			Content: msg.TextContent(),
		})
	}
	return out
}

func toolDryRuns(defs []tool.Definition) []ToolDryRun {
	out := make([]ToolDryRun, 0, len(defs))
	for _, def := range defs {
		out = append(out, ToolDryRun{Name: def.Name, Description: def.Description})
	}
	return out
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
