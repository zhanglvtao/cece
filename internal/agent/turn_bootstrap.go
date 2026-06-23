package agent

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type TurnBootstrap struct {
	engine             TurnEngine
	sessionCoordinator *SessionCoordinator
	confirmCh          <-chan struct{}
	rejectCh           <-chan struct{}
}

func NewTurnBootstrap(engine TurnEngine, sessionCoordinator *SessionCoordinator, confirmCh <-chan struct{}, rejectCh <-chan struct{}) *TurnBootstrap {
	return &TurnBootstrap{engine: engine, sessionCoordinator: sessionCoordinator, confirmCh: confirmCh, rejectCh: rejectCh}
}

func (b *TurnBootstrap) Run(ctx context.Context, input string, user Message, snapshot []Message, newSession SessionStartResult, events chan<- Event) {
	if newSession.ID != "" {
		events <- SessionCreated{ID: newSession.ID, Title: newSession.Title}
	}
	events <- UserMessageAdded{Message: user}

	plan := b.BuildTurnPlan(input, snapshot)
	runner := NewTurnRunner(
		b.newModelStreamer(),
		b.newInteractionGate(),
		b.newToolExecutor(),
		b.engine.MaxTokens(),
		b.turnDeps(),
	)
	runner.Run(ctx, plan, events)
}

func (b *TurnBootstrap) BuildTurnPlan(input string, snapshot []Message) TurnPlan {
	eng := b.engine
	assembleResult := prompt.AssembleResult{}
	if eng.Assembler() != nil {
		turnCtx := prompt.TurnContext{
			IncludeTime:             prompt.ShouldInjectTime(input),
			Now:                     time.Now(),
			CurrentWorkingDirectory: eng.ProjectDir(),
			CurrentBranch:           currentBranch(eng.ProjectDir()),
			Mode:                    "interactive",
			PermissionMode:          permissionMode(eng.PlanState()),
			Yolo:                    eng.Yolo(),
			ConversationTurnNumber:  eng.HistoryLen()/2 + 1,
			TaskReminder:            prompt.TaskReminderForInput(input),
		}
		assembleResult = eng.Assembler().Assemble(turnCtx)
	}
	systemPrompt := AssembleResultToSystemPrompt(assembleResult)
	tools := toolDefinitions(eng.Registry())
	return TurnPlan{
		Messages:       snapshot,
		System:         systemPrompt,
		AssembleResult: assembleResult,
		Tools:          tools,
	}
}

func permissionMode(planState *tool.PlanModeState) string {
	if planState == nil {
		return string(tool.PermissionModeDefault)
	}
	return string(planState.Mode())
}

func currentBranch(projectDir string) string {
	if strings.TrimSpace(projectDir) == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", projectDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (b *TurnBootstrap) BuildDryRunRequest(input string, plan TurnPlan) RequestDryRun {
	return RequestDryRun{
		Input:                input,
		MaxTokens:            b.engine.MaxTokens(),
		EstimatedInputTokens: EstimateRequestTokens(plan.System, plan.Messages, plan.Tools),
		PromptLayers:         promptLayerDryRuns(plan.AssembleResult),
		Messages:             messageDryRuns(plan.Messages),
		Tools:                toolDryRuns(plan.Tools),
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

func toolDryRuns(defs []tool.Definition) []ToolDryRun {
	out := make([]ToolDryRun, 0, len(defs))
	for _, def := range defs {
		out = append(out, ToolDryRun{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema})
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

func (b *TurnBootstrap) newModelStreamer() *ModelStreamer {
	eng := b.engine
	return NewModelStreamer(eng.Client(), eng.Registry(), func(inputTokens int) {
		eng.SetLastInputTokens(inputTokens)
	})
}

func (b *TurnBootstrap) newInteractionGate() *InteractionGate {
	eng := b.engine
	return NewInteractionGate(eng.Registry(), eng.PlanState(), eng.Yolo(), b.confirmCh, b.rejectCh, func() {
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
			if b.sessionCoordinator != nil {
				b.sessionCoordinator.UpdateMeta(ctx, sessionID, meta)
			}
			eng.RecordUsage(ctx, UsageRecord{
				SessionID:                sessionID,
				Model:                    meta.Model,
				InputTokens:              resp.inputTokens,
				OutputTokens:             resp.outputTokens,
				CacheReadTokens:          resp.cacheReadTokens,
				CacheCreationTokens:      resp.cacheCreationTokens,
				TotalInputTokens:         meta.TotalInputTokens,
				TotalOutputTokens:        meta.TotalOutputTokens,
				TotalCacheReadTokens:     meta.StatusBar.CacheReadTokens,
				TotalCacheCreationTokens: meta.StatusBar.CacheCreationTokens,
			})
		},
		DrainQueuedInputs:   eng.DrainQueuedInputs,
		DrainModeReminder:   eng.PlanState().DrainModeReminder,
		TryAutoCompact:      eng.TryAutoCompact,
		HistorySnapshot:     eng.HistorySnapshot,
		IncrementAPICalls:   eng.IncrementAPICalls,
		RecordToolExecution: eng.RecordToolExecution,
		UpdateCacheTokens:   eng.UpdateCacheTokens,
		ContextWindow:       eng.ContextWindow(),
	}
}

func (b *TurnBootstrap) updateTokenCounts(resp modelResponse) (string, session.SessionMeta, bool) {
	return b.engine.IncrementTokens(resp.inputTokens, resp.outputTokens)
}
