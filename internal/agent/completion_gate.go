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
	checks := []CompletionGateCheck{
		evaluatePlanModeGate(ctx),
		evaluateTodoGate(ctx),
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

func buildCompletionGateReminder(reasons []string) string {
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("Completion gate blocked turn completion:\n")
	for _, reason := range reasons {
		b.WriteString("- ")
		b.WriteString(reason)
		b.WriteByte('\n')
	}
	b.WriteString("Continue the task. Either finish outstanding work, update Todo/plan state, or ask the user if blocked.\n")
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
	b.WriteString("Do not answer with plain text. Take a state-changing action now: Todo, AskUserQuestion, or ExitPlanMode. If the task cannot continue, ask the user for direction.\n")
	b.WriteString("</system-reminder>")
	return b.String()
}
