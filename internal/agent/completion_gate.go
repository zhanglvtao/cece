package agent

import (
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/tool"
)

type ClosureEvidenceKind string

const (
	ClosureEvidenceCodeChange   ClosureEvidenceKind = "code_change"
	ClosureEvidenceVerification ClosureEvidenceKind = "verification"
	ClosureEvidenceRead         ClosureEvidenceKind = "read"
)

type ClosureEvidence struct {
	ToolUseID string
	Kind      ClosureEvidenceKind
	ToolName  string
	OK        bool
	Command   string
	Summary   string
}

type CompletionGateContext struct {
	PlanMode        bool
	TaskList        []tool.TodoItem
	RequiresClosure bool
	Closure         tool.TaskClosureSnapshot
	Evidence        []ClosureEvidence
}

type CompletionGateResult struct {
	Pass     bool
	Reasons  []string
	Reminder string
}

type CompletionGate struct{}

func NewCompletionGate() *CompletionGate { return &CompletionGate{} }

func (g *CompletionGate) Evaluate(ctx CompletionGateContext) CompletionGateResult {
	var reasons []string
	if ctx.PlanMode {
		reasons = append(reasons, "PlanModeGate: plan mode is still active; end with AskUserQuestion or ExitPlanMode instead of plain text.")
	}
	for _, item := range ctx.TaskList {
		if item.Status == tool.TodoPending || item.Status == tool.TodoInProgress {
			reasons = append(reasons, fmt.Sprintf("TodoGate: task %q is still %s.", item.Content, item.Status))
		}
	}
	if ctx.RequiresClosure {
		reasons = append(reasons, evaluateTaskClosure(ctx.Closure, ctx.Evidence)...)
	}
	if len(reasons) == 0 {
		return CompletionGateResult{Pass: true}
	}
	return CompletionGateResult{Pass: false, Reasons: reasons, Reminder: buildCompletionGateReminder(reasons)}
}

func evaluateTaskClosure(closure tool.TaskClosureSnapshot, evidence []ClosureEvidence) []string {
	var reasons []string
	if !closure.Updated {
		return []string{"TaskClosureGate: call UpdateTaskClosure before ending this implementation task."}
	}
	if closure.NeedsCodeChange == tool.ClosureDecisionUnknown {
		reasons = append(reasons, "TaskClosureGate: needs_code_change is unknown.")
	}
	if closure.NeedsVerification == tool.ClosureDecisionUnknown {
		reasons = append(reasons, "TaskClosureGate: needs_verification is unknown.")
	}
	if len(closure.RemainingWork) > 0 {
		reasons = append(reasons, "TaskClosureGate: remaining_work is not empty.")
	}
	if needsReasonForCode(closure) && strings.TrimSpace(closure.CodeChangeReason) == "" {
		reasons = append(reasons, "TaskClosureGate: code_change_reason is required.")
	}
	if needsReasonForVerification(closure) && strings.TrimSpace(closure.VerificationReason) == "" {
		reasons = append(reasons, "TaskClosureGate: verification_reason is required.")
	}
	if closure.NeedsCodeChange == tool.ClosureDecisionYes && closure.CodeChangeStatus != tool.ClosureCodeBlocked {
		if closure.CodeChangeStatus != tool.ClosureCodeChanged {
			reasons = append(reasons, "TaskClosureGate: needs_code_change=yes requires code_change_status=changed or blocked.")
		} else if !hasMatchingEvidenceRef(closure.CodeChangeToolResultRefs, evidence, ClosureEvidenceCodeChange, true) {
			reasons = append(reasons, "TaskClosureGate: needs_code_change=yes requires code_change_tool_result_refs to reference a successful Edit/Write tool result from this turn.")
		}
	}
	if closure.NeedsVerification == tool.ClosureDecisionYes && closure.VerificationStatus != tool.ClosureVerificationBlocked {
		if closure.VerificationStatus != tool.ClosureVerificationPassed {
			reasons = append(reasons, "TaskClosureGate: needs_verification=yes requires verification_status=passed or blocked.")
		} else if !hasMatchingEvidenceRef(closure.VerificationToolResultRefs, evidence, ClosureEvidenceVerification, true) {
			reasons = append(reasons, "TaskClosureGate: needs_verification=yes requires verification_tool_result_refs to reference a validation Bash tool result from this turn.")
		}
	}
	return reasons
}

func needsReasonForCode(closure tool.TaskClosureSnapshot) bool {
	return closure.NeedsCodeChange == tool.ClosureDecisionNo || closure.CodeChangeStatus == tool.ClosureCodeNotNeeded || closure.CodeChangeStatus == tool.ClosureCodeBlocked
}

func needsReasonForVerification(closure tool.TaskClosureSnapshot) bool {
	return closure.NeedsVerification == tool.ClosureDecisionNo || closure.VerificationStatus == tool.ClosureVerificationNotNeeded || closure.VerificationStatus == tool.ClosureVerificationBlocked
}

func hasMatchingEvidenceRef(refs []string, evidence []ClosureEvidence, kind ClosureEvidenceKind, requireOK bool) bool {
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		for _, ev := range evidence {
			if ev.ToolUseID != ref || ev.Kind != kind {
				continue
			}
			if requireOK && !ev.OK {
				continue
			}
			return true
		}
	}
	return false
}

func buildCompletionGateReminder(reasons []string) string {
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("Completion gate blocked turn completion:\n")
	for _, reason := range reasons {
		b.WriteString("- ")
		b.WriteString(reason)
		b.WriteByte('\n')
	}
	b.WriteString("Continue the task. Either perform the missing work and update TaskClosure, or mark it blocked with a concrete reason.\n")
	b.WriteString("</system-reminder>")
	return b.String()
}

func RequiresTaskClosure(input string) bool {
	text := strings.ToLower(input)
	if strings.Contains(text, "<loaded_skill>") {
		return false
	}
	keywords := []string{
		"implement", "add", "update", "refactor", "fix", "bug", "test", "build", "failing", "failed", "failure", "error", "exception",
		"实现", "修改", "修复", "报错", "失败", "异常", "回归", "构建", "测试",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
