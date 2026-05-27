package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"cece/internal/tool"
)

// InteractionGate handles user-facing interruptions before tool execution.
type InteractionGate struct {
	registry             *tool.Registry
	planState            *tool.PlanModeState
	yolo                 bool
	confirmCh            <-chan struct{}
	resetQuestionAnswers func()
}

func NewInteractionGate(registry *tool.Registry, planState *tool.PlanModeState, yolo bool, confirmCh <-chan struct{}, resetQuestionAnswers func()) *InteractionGate {
	return &InteractionGate{
		registry:             registry,
		planState:            planState,
		yolo:                 yolo,
		confirmCh:            confirmCh,
		resetQuestionAnswers: resetQuestionAnswers,
	}
}

func (g *InteractionGate) WaitIfNeeded(ctx context.Context, calls []ApiToolUseBlock, events chan<- Event) error {
	if g.yolo || g.isAutoAccept() || shouldAutoApproveToolCalls(calls) {
		// Auto-approve: yolo mode or single EnterPlanMode.
		return nil
	}
	if g.isWriteEffectFree(calls) {
		// Auto-approve in Default mode: read/exec/mode tools (no write).
		return nil
	}
	if g.isPlansDirOnlyWrites(calls) {
		// Auto-approve in Plan mode: all writes target the plans directory.
		return nil
	}
	if g.isPlanMode() && g.hasWriteEffectTools(calls) {
		// Plan mode + write outside plans dir: skip UI prompt, let ToolExecutor return denial.
		return nil
	}
	if hasExitPlanMode(calls) {
		planContent, planFile := exitPlanModePreview(calls)
		events <- UIPlanApprovalRequested{
			PlanContent: planContent,
			PlanFile:    planFile,
		}
		return g.wait(ctx)
	}
	if hasAskUserQuestion(calls) {
		questions := parseAskUserQuestionCalls(calls)
		if g.resetQuestionAnswers != nil {
			g.resetQuestionAnswers()
		}
		events <- UIQuestionAsked{
			CallID:    calls[0].ID,
			Questions: questions,
		}
		return g.wait(ctx)
	}

	events <- UIToolCallsReady{Calls: calls}
	return g.wait(ctx)
}

func (g *InteractionGate) wait(ctx context.Context) error {
	select {
	case <-g.confirmCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func shouldAutoApproveToolCalls(calls []ApiToolUseBlock) bool {
	return len(calls) == 1 && calls[0].Name == tool.EnterPlanModeToolName
}

func hasExitPlanMode(calls []ApiToolUseBlock) bool {
	for _, tc := range calls {
		if tc.Name == tool.ExitPlanModeToolName {
			return true
		}
	}
	return false
}

func hasAskUserQuestion(calls []ApiToolUseBlock) bool {
	for _, tc := range calls {
		if tc.Name == tool.AskUserQuestionToolName {
			return true
		}
	}
	return false
}

// isAutoAccept returns true when the current permission mode is auto-accept.
func (g *InteractionGate) isAutoAccept() bool {
	if g.planState == nil {
		return false
	}
	return g.planState.Mode() == tool.PermissionModeAutoAccept
}

// isWriteEffectFree returns true when no tool call in the batch has a write effect.
// Write-effect tools (Edit, Write) still require UI confirmation (or are denied in Plan mode).
// EnterPlanMode / ExitPlanMode are special-case mode tools that have their own approval flow;
// they return false so the caller routes through plan-approval or confirmation dialogs.
func (g *InteractionGate) isWriteEffectFree(calls []ApiToolUseBlock) bool {
	for _, c := range calls {
		t, ok := g.registry.Get(c.Name)
		if !ok {
			return false
		}
		if tool.EffectOf(t) == tool.EffectWrite {
			return false
		}
		if tool.EffectOf(t) == tool.EffectMode {
			return false
		}
	}
	return true
}

// isPlanMode returns true when the current permission mode is plan.
func (g *InteractionGate) isPlanMode() bool {
	if g.planState == nil {
		return false
	}
	return g.planState.Mode() == tool.PermissionModePlan
}

// hasWriteEffectTools returns true when any tool call in the batch has a write effect.
func (g *InteractionGate) hasWriteEffectTools(calls []ApiToolUseBlock) bool {
	for _, c := range calls {
		t, ok := g.registry.Get(c.Name)
		if !ok {
			continue
		}
		if tool.EffectOf(t) == tool.EffectWrite {
			return true
		}
	}
	return false
}

// isPlansDirOnlyWrites returns true when every write-effect tool call in the batch
// targets a path under the .cece/plans/ directory. Non-write-effect tools are ignored.
// Returns false if any write-effect tool targets outside plans dir or if plansDir is empty.
func (g *InteractionGate) isPlansDirOnlyWrites(calls []ApiToolUseBlock) bool {
	plansDir := ""
	if g.planState != nil {
		plansDir = g.planState.PlansDir()
	}
	if plansDir == "" {
		return false
	}
	hasWrite := false
	for _, c := range calls {
		t, ok := g.registry.Get(c.Name)
		if !ok {
			return false
		}
		if tool.EffectOf(t) == tool.EffectWrite {
			hasWrite = true
			if !isPlansDirWriteInput(plansDir, c.Input) {
				return false
			}
		}
	}
	return hasWrite
}

func exitPlanModePreview(calls []ApiToolUseBlock) (planContent, planFile string) {
	for _, tc := range calls {
		if tc.Name != tool.ExitPlanModeToolName {
			continue
		}
		var args struct {
			PlanFile string `json:"plan_file"`
		}
		if json.Unmarshal(tc.Input, &args) == nil && args.PlanFile != "" {
			planFile = filepath.Base(args.PlanFile)
			abs, _ := filepath.Abs(args.PlanFile)
			data, err := os.ReadFile(abs)
			if err == nil {
				planContent = string(data)
			}
		}
		break
	}
	return planContent, planFile
}

func parseAskUserQuestionCalls(calls []ApiToolUseBlock) []tool.Question {
	var questions []tool.Question
	for _, tc := range calls {
		if tc.Name != tool.AskUserQuestionToolName {
			continue
		}
		parsed, err := parseAskUserQuestions(tc.Input)
		if err != nil {
			questions = append(questions, tool.Question{Question: "Error parsing questions"})
		} else {
			questions = append(questions, parsed...)
		}
	}
	return questions
}
