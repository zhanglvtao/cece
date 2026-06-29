package agent

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestCompletionGateReportsStructuredChecks(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass: %q", result.Reminder)
	}
	if len(result.Checks) != 2 {
		t.Fatalf("checks len = %d, want 2", len(result.Checks))
	}
	if result.Checks[0].Name != "PlanModeGate" || result.Checks[0].Status != CompletionGatePassed {
		t.Fatalf("plan check = %+v", result.Checks[0])
	}
	if result.Checks[1].Name != "TodoGate" || result.Checks[1].Status != CompletionGatePassed {
		t.Fatalf("todo check = %+v", result.Checks[1])
	}
}

func TestCompletionGateIgnoresTaskClosureEvidence(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		RequiresClosure: true,
		Closure:         tool.TaskClosureSnapshot{},
		Evidence:        []ClosureEvidence{{ToolUseID: "call_edit", Kind: ClosureEvidenceCodeChange, ToolName: "Edit", OK: true}},
	})

	if !result.Pass {
		t.Fatalf("gate blocked, want pass without task closure gate: %q", result.Reminder)
	}
	if len(result.Checks) != 2 {
		t.Fatalf("checks len = %d, want 2", len(result.Checks))
	}
	if strings.Contains(result.Reminder, "UpdateTaskClosure") {
		t.Fatalf("reminder %q should not mention UpdateTaskClosure", result.Reminder)
	}
}

func TestCompletionGateBlocksPlanMode(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{PlanMode: true})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if result.Checks[0].Name != "PlanModeGate" || result.Checks[0].Status != CompletionGateBlocked {
		t.Fatalf("plan check = %+v, want blocked", result.Checks[0])
	}
	if !strings.Contains(result.Reminder, "PlanModeGate") {
		t.Fatalf("Reminder = %q, want PlanModeGate reason", result.Reminder)
	}
	if strings.Contains(result.Reminder, "UpdateTaskClosure") {
		t.Fatalf("Reminder = %q, should not mention UpdateTaskClosure", result.Reminder)
	}
}

func TestCompletionGateBlocksUnfinishedTodo(t *testing.T) {
	gate := NewCompletionGate()
	result := gate.Evaluate(CompletionGateContext{
		TaskList: []tool.TodoItem{{Content: "Run tests", Status: tool.TodoInProgress}},
	})

	if result.Pass {
		t.Fatal("gate passed, want blocked")
	}
	if result.Checks[1].Name != "TodoGate" || result.Checks[1].Status != CompletionGateBlocked {
		t.Fatalf("todo check = %+v, want blocked", result.Checks[1])
	}
	if !strings.Contains(result.Reminder, "TodoGate") {
		t.Fatalf("Reminder = %q, want TodoGate reason", result.Reminder)
	}
	if strings.Contains(result.Reminder, "UpdateTaskClosure") {
		t.Fatalf("Reminder = %q, should not mention UpdateTaskClosure", result.Reminder)
	}
}

func TestBuildCompletionGateNoProgressReminder(t *testing.T) {
	reminder := buildCompletionGateNoProgressReminder([]string{"TodoGate: task \"x\" is still in_progress."})
	if !strings.Contains(reminder, "Do not answer with plain text") {
		t.Fatalf("reminder %q missing no-progress instruction", reminder)
	}
	if !strings.Contains(reminder, "Todo") || !strings.Contains(reminder, "AskUserQuestion") || !strings.Contains(reminder, "ExitPlanMode") {
		t.Fatalf("reminder %q missing required tool guidance", reminder)
	}
	if strings.Contains(reminder, "UpdateTaskClosure") {
		t.Fatalf("reminder %q should not mention UpdateTaskClosure", reminder)
	}
}
