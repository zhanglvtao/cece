package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/zhanglvtao/cece/internal/tool"
)

// InteractionGate handles user-facing interruptions before tool execution.
type InteractionGate struct {
	registry             *tool.Registry
	planState            *tool.PlanModeState
	yolo                 bool
	confirmCh            <-chan struct{}
	rejectCh             <-chan struct{}
	resetQuestionAnswers func()
}

func NewInteractionGate(registry *tool.Registry, planState *tool.PlanModeState, yolo bool, confirmCh <-chan struct{}, rejectCh <-chan struct{}, resetQuestionAnswers func()) *InteractionGate {
	return &InteractionGate{
		registry:             registry,
		planState:            planState,
		yolo:                 yolo,
		confirmCh:            confirmCh,
		rejectCh:             rejectCh,
		resetQuestionAnswers: resetQuestionAnswers,
	}
}

// WaitRejected indicates the user rejected the interaction (plan, tool calls, or question).
// The caller should construct rejection tool_results and continue the agent loop.
var WaitRejected = errors.New("interaction rejected by user")

func (g *InteractionGate) WaitIfNeeded(ctx context.Context, calls []ApiToolUseBlock, events chan<- Event) error {
	if g.yolo || g.isAutoAccept() || shouldAutoApproveToolCalls(calls) {
		// Auto-approve: yolo mode, auto-accept mode, or single EnterPlanMode.
		return nil
	}

	// In default mode: all tools auto-approve unless LLM explicitly marks
	// require_confirmation=true in the tool input.
	if g.isDefaultMode() {
		if hasExplicitConfirmation(calls) {
			events <- ToolCallsReady{Calls: calls}
			return g.wait(ctx)
		}
		return nil
	}

	// ExitPlanMode always requires explicit user approval, even when
	// combined with other read-only / mode-effect tools.
	if hasExitPlanMode(calls) {
		planContent, planFile := exitPlanModePreview(calls)
		events <- PlanApprovalRequested{
			PlanContent: planContent,
			PlanFile:    planFile,
		}
		if err := g.wait(ctx); err != nil {
			return err
		}
		return nil
	}

	if g.isPlanMode() && g.hasOnlyReadOnlyCalls(calls) {
		// Auto-approve read/exec/mode-effect tools in plan mode.
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
	if hasAskUserQuestion(calls) {
		questions := parseAskUserQuestionCalls(calls)
		if g.resetQuestionAnswers != nil {
			g.resetQuestionAnswers()
		}
		events <- QuestionAsked{
			CallID:    calls[0].ID,
			Questions: questions,
		}
		return g.wait(ctx)
	}

	events <- ToolCallsReady{Calls: calls}
	return g.wait(ctx)
}

// hasOnlyReadOnlyCalls returns true when every tool call in the batch is a
// read-effect, mode-effect, or exec-effect (Bash) tool. In plan mode these
// are safe to auto-approve because the ToolExecutor still blocks write-effect
// tools and the system prompt constrains Bash to read-only commands.
func (g *InteractionGate) hasOnlyReadOnlyCalls(calls []ApiToolUseBlock) bool {
	for _, c := range calls {
		t, ok := g.registry.Get(c.Name)
		if !ok {
			return false
		}
		eff := tool.EffectOf(t)
		if eff == tool.EffectWrite {
			return false
		}
	}
	return true
}

func (g *InteractionGate) wait(ctx context.Context) error {
	select {
	case <-g.confirmCh:
		return nil
	case <-g.rejectCh:
		return WaitRejected
	case <-ctx.Done():
		return ctx.Err()
	}
}

func shouldAutoApproveToolCalls(calls []ApiToolUseBlock) bool {
	if len(calls) != 1 {
		return false
	}
	name := calls[0].Name
	return name == tool.EnterPlanModeToolName || name == tool.TodoToolName
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

// isDefaultMode returns true when the current permission mode is default.
func (g *InteractionGate) isDefaultMode() bool {
	if g.planState == nil {
		return true
	}
	return g.planState.Mode() == tool.PermissionModeDefault
}

// hasExplicitConfirmation checks if any tool call has require_confirmation=true in its input.
func hasExplicitConfirmation(calls []ApiToolUseBlock) bool {
	for _, c := range calls {
		var params struct {
			RequireConfirmation bool `json:"require_confirmation"`
		}
		if json.Unmarshal(c.Input, &params) == nil && params.RequireConfirmation {
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
