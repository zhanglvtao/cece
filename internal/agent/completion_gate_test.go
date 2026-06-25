package agent

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

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
