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

type CompletionGateResult struct {
	Pass     bool
	Checks   []CompletionGateCheck
	Reasons  []string
	Reminder string
}

type CompletionGate struct{}

func NewCompletionGate() *CompletionGate { return &CompletionGate{} }

func (g *CompletionGate) Evaluate(ctx CompletionGateContext) CompletionGateResult {
	ctx.RequiresClosure = completionGateRequiresClosure(ctx)
	checks := []CompletionGateCheck{
		evaluatePlanModeGate(ctx),
		evaluateTodoGate(ctx),
		evaluateTaskClosureGate(ctx),
	}
	var reasons []string
	for _, check := range checks {
		if check.Status != CompletionGateBlocked {
			continue
		}
		if len(check.Details) == 0 {
			reasons = append(reasons, check.Name+": "+check.Summary)
			continue
		}
		for _, detail := range check.Details {
			reasons = append(reasons, check.Name+": "+detail)
		}
	}
	if len(reasons) == 0 {
		return CompletionGateResult{Pass: true, Checks: checks}
	}
	return CompletionGateResult{Pass: false, Checks: checks, Reasons: reasons, Reminder: buildCompletionGateReminder(reasons)}
}

func completionGateRequiresClosure(ctx CompletionGateContext) bool {
	return ctx.RequiresClosure || ctx.Closure.Updated || hasSuccessfulCodeChangeEvidence(ctx.Evidence)
}

func hasSuccessfulCodeChangeEvidence(evidence []ClosureEvidence) bool {
	for _, ev := range evidence {
		if ev.Kind == ClosureEvidenceCodeChange && ev.OK {
			return true
		}
	}
	return false
}

func evaluatePlanModeGate(ctx CompletionGateContext) CompletionGateCheck {
	if ctx.PlanMode {
		return CompletionGateCheck{Name: "PlanModeGate", Status: CompletionGateBlocked, Summary: "plan mode active", Details: []string{"plan mode is still active; end with AskUserQuestion or ExitPlanMode instead of plain text."}}
	}
	return CompletionGateCheck{Name: "PlanModeGate", Status: CompletionGatePassed, Summary: "default mode"}
}

func evaluateTodoGate(ctx CompletionGateContext) CompletionGateCheck {
	pending := 0
	inProgress := 0
	var details []string
	for _, item := range ctx.TaskList {
		switch item.Status {
		case tool.TodoPending:
			pending++
			details = append(details, fmt.Sprintf("task %q is still pending.", item.Content))
		case tool.TodoInProgress:
			inProgress++
			details = append(details, fmt.Sprintf("task %q is still in_progress.", item.Content))
		}
	}
	if pending == 0 && inProgress == 0 {
		return CompletionGateCheck{Name: "TodoGate", Status: CompletionGatePassed, Summary: "all done"}
	}
	summaryParts := []string{}
	if inProgress > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d in_progress", inProgress))
	}
	if pending > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d pending", pending))
	}
	return CompletionGateCheck{Name: "TodoGate", Status: CompletionGateBlocked, Summary: strings.Join(summaryParts, ", "), Details: details}
}

func evaluateTaskClosureGate(ctx CompletionGateContext) CompletionGateCheck {
	if !ctx.RequiresClosure {
		return CompletionGateCheck{Name: "TaskClosureGate", Status: CompletionGateSkipped, Summary: "not required"}
	}
	reasons := evaluateTaskClosure(ctx.Closure, ctx.Evidence)
	if len(reasons) == 0 {
		return CompletionGateCheck{Name: "TaskClosureGate", Status: CompletionGatePassed, Summary: "closure valid"}
	}
	details := make([]string, len(reasons))
	for i, reason := range reasons {
		details[i] = strings.TrimPrefix(reason, "TaskClosureGate: ")
	}
	return CompletionGateCheck{Name: "TaskClosureGate", Status: CompletionGateBlocked, Summary: details[0], Details: details}
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
	b.WriteString("Continue the task. Either perform the missing work, update Todo/plan state, or call UpdateTaskClosure to finish explicitly. If the task should stop, use blocked or not_needed with a concrete reason instead of plain text.\n")
	b.WriteString("</system-reminder>")
	return b.String()
}

func buildCompletionGateNoProgressReminder(reasons []string) string {
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("Completion gate is still blocked and no progress was made:\n")
	for _, reason := range reasons {
		b.WriteString("- ")
		b.WriteString(reason)
		b.WriteByte('\n')
	}
	b.WriteString("Do not answer with plain text. You must take a state-changing action now: call UpdateTaskClosure, Todo, AskUserQuestion, or ExitPlanMode. If the task cannot continue, call UpdateTaskClosure with blocked or not_needed and a concrete reason.\n")
	b.WriteString("</system-reminder>")
	return b.String()
}
