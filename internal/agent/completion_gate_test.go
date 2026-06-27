package agent

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestCompletionGateReportsStructuredChecks(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure: tool.TaskClosureSnapshot{
			Updated:                    true,
			NeedsCodeChange:            tool.ClosureDecisionYes,
			CodeChangeStatus:           tool.ClosureCodeChanged,
			CodeChangeReason:           "changed code",
			CodeChangeToolResultRefs:   []string{"call_edit"},
			NeedsVerification:          tool.ClosureDecisionYes,
			VerificationStatus:         tool.ClosureVerificationPassed,
			VerificationReason:         "tests passed",
			VerificationToolResultRefs: []string{"call_test"},
		},
		Evidence: []ClosureEvidence{
			{ToolUseID: "call_edit", Kind: ClosureEvidenceCodeChange, ToolName: "Edit", OK: true},
			{ToolUseID: "call_test", Kind: ClosureEvidenceVerification, ToolName: "Bash", OK: true},
		},
	})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks len = %d, want 3", len(result.Checks))
	}
	if result.Checks[0].Name != "PlanModeGate" || result.Checks[0].Status != CompletionGatePassed {
		t.Fatalf("plan check = %+v", result.Checks[0])
	}
	if result.Checks[1].Name != "TodoGate" || result.Checks[1].Status != CompletionGatePassed {
		t.Fatalf("todo check = %+v", result.Checks[1])
	}
	if result.Checks[2].Name != "TaskClosureGate" || result.Checks[2].Status != CompletionGatePassed {
		t.Fatalf("closure check = %+v", result.Checks[2])
	}
}

func TestCompletionGateSkipsTaskClosureWhenNotRequired(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{})
	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks len = %d, want 3", len(result.Checks))
	}
	closure := result.Checks[2]
	if closure.Name != "TaskClosureGate" || closure.Status != CompletionGateSkipped || closure.Summary != "not required" {
		t.Fatalf("closure check = %+v", closure)
	}
}

func TestCompletionGateRequiresTaskClosureAfterCodeChangeEvidence(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		Evidence: []ClosureEvidence{{ToolUseID: "call_edit", Kind: ClosureEvidenceCodeChange, ToolName: "Edit", OK: true}},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	closure := result.Checks[2]
	if closure.Name != "TaskClosureGate" || closure.Status != CompletionGateBlocked {
		t.Fatalf("closure check = %+v, want blocked", closure)
	}
	if !strings.Contains(result.Reminder, "UpdateTaskClosure") {
		t.Fatalf("Reminder = %q, want UpdateTaskClosure reason", result.Reminder)
	}
}

func TestCompletionGateValidatesModelProvidedTaskClosure(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		Closure: tool.TaskClosureSnapshot{
			Updated:            true,
			NeedsCodeChange:    tool.ClosureDecisionNo,
			CodeChangeStatus:   tool.ClosureCodeNotNeeded,
			CodeChangeReason:   "read-only answer",
			NeedsVerification:  tool.ClosureDecisionNo,
			VerificationStatus: tool.ClosureVerificationNotNeeded,
			VerificationReason: "not needed",
			RemainingWork:      []string{"explain the unresolved part"},
		},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if !strings.Contains(result.Reminder, "remaining_work") {
		t.Fatalf("Reminder = %q, want remaining_work reason", result.Reminder)
	}
}

func TestCompletionGateValidatesCodeChangeClosureInferredFromEvidence(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		Closure: tool.TaskClosureSnapshot{
			Updated:                  true,
			NeedsCodeChange:          tool.ClosureDecisionYes,
			CodeChangeStatus:         tool.ClosureCodeChanged,
			CodeChangeReason:         "changed code",
			CodeChangeToolResultRefs: []string{"call_edit"},
			NeedsVerification:        tool.ClosureDecisionNo,
			VerificationStatus:       tool.ClosureVerificationNotNeeded,
			VerificationReason:       "not needed",
		},
		Evidence: []ClosureEvidence{{ToolUseID: "call_edit", Kind: ClosureEvidenceCodeChange, ToolName: "Edit", OK: true}},
	})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
	closure := result.Checks[2]
	if closure.Name != "TaskClosureGate" || closure.Status != CompletionGatePassed {
		t.Fatalf("closure check = %+v, want passed", closure)
	}
}

func TestCompletionGateBlockedChecksFeedReminder(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure:         tool.TaskClosureSnapshot{},
	})
	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks len = %d, want 3", len(result.Checks))
	}
	closure := result.Checks[2]
	if closure.Status != CompletionGateBlocked || len(closure.Details) == 0 {
		t.Fatalf("closure check = %+v", closure)
	}
	if !strings.Contains(result.Reminder, closure.Details[0]) {
		t.Fatalf("reminder %q missing detail %q", result.Reminder, closure.Details[0])
	}
	if !strings.Contains(result.Reminder, "UpdateTaskClosure") || !strings.Contains(result.Reminder, "blocked") || !strings.Contains(result.Reminder, "not_needed") {
		t.Fatalf("reminder %q missing self-termination guidance", result.Reminder)
	}
}
func TestCompletionGateBlocksPendingTodo(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		TaskList: []tool.TodoItem{{Content: "Run tests", Status: tool.TodoInProgress}},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if !strings.Contains(result.Reminder, "TodoGate") {
		t.Fatalf("Reminder = %q, want TodoGate reason", result.Reminder)
	}
}

func TestCompletionGateBlocksMissingTaskClosure(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure:         tool.TaskClosureSnapshot{},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if !strings.Contains(result.Reminder, "UpdateTaskClosure") {
		t.Fatalf("Reminder = %q, want UpdateTaskClosure reason", result.Reminder)
	}
}

func TestCompletionGateRequiresCodeChangeRef(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure: tool.TaskClosureSnapshot{
			Updated:                  true,
			NeedsCodeChange:          tool.ClosureDecisionYes,
			CodeChangeStatus:         tool.ClosureCodeChanged,
			CodeChangeReason:         "changed code",
			CodeChangeToolResultRefs: []string{"call_read"},
			NeedsVerification:        tool.ClosureDecisionNo,
			VerificationStatus:       tool.ClosureVerificationNotNeeded,
			VerificationReason:       "not needed",
		},
		Evidence: []ClosureEvidence{{ToolUseID: "call_read", Kind: ClosureEvidenceRead, ToolName: "Read", OK: true}},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if !strings.Contains(result.Reminder, "code_change_tool_result_refs") {
		t.Fatalf("Reminder = %q, want code refs reason", result.Reminder)
	}
}

func TestCompletionGatePassesWithValidTaskClosureRefs(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure: tool.TaskClosureSnapshot{
			Updated:                    true,
			NeedsCodeChange:            tool.ClosureDecisionYes,
			CodeChangeStatus:           tool.ClosureCodeChanged,
			CodeChangeReason:           "changed code",
			CodeChangeToolResultRefs:   []string{"call_edit"},
			NeedsVerification:          tool.ClosureDecisionYes,
			VerificationStatus:         tool.ClosureVerificationPassed,
			VerificationReason:         "tests passed",
			VerificationToolResultRefs: []string{"call_test"},
		},
		Evidence: []ClosureEvidence{
			{ToolUseID: "call_edit", Kind: ClosureEvidenceCodeChange, ToolName: "Edit", OK: true},
			{ToolUseID: "call_test", Kind: ClosureEvidenceVerification, ToolName: "Bash", OK: true, Command: "go test ./internal/agent"},
		},
	})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
}

func TestCompletionGatePassesBlockedClosureWithReason(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure: tool.TaskClosureSnapshot{
			Updated:            true,
			NeedsCodeChange:    tool.ClosureDecisionYes,
			CodeChangeStatus:   tool.ClosureCodeBlocked,
			CodeChangeReason:   "repository is read-only",
			NeedsVerification:  tool.ClosureDecisionYes,
			VerificationStatus: tool.ClosureVerificationBlocked,
			VerificationReason: "test dependency is unavailable",
		},
	})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
}

func TestBuildCompletionGateNoProgressReminder(t *testing.T) {
	reminder := buildCompletionGateNoProgressReminder([]string{"TodoGate: task \"x\" is still in_progress."})
	if !strings.Contains(reminder, "Do not answer with plain text") {
		t.Fatalf("reminder %q missing no-progress instruction", reminder)
	}
	if !strings.Contains(reminder, "UpdateTaskClosure") || !strings.Contains(reminder, "Todo") || !strings.Contains(reminder, "ExitPlanMode") {
		t.Fatalf("reminder %q missing required tool guidance", reminder)
	}
}
