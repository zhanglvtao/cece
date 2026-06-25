package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateTaskClosureStoresSnapshot(t *testing.T) {
	state := NewTaskClosureState()
	input := map[string]any{
		"needs_code_change":             "yes",
		"code_change_status":            "changed",
		"code_change_reason":            "updated turn runner",
		"code_change_tool_result_refs":  []string{"call_edit"},
		"needs_verification":            "yes",
		"verification_status":           "passed",
		"verification_reason":           "agent tests passed",
		"verification_tool_result_refs": []string{"call_test"},
		"remaining_work":                []string{},
	}
	b, _ := json.Marshal(input)

	result := NewTaskClosure(state).Run(context.Background(), b, nil)
	if result.IsError {
		t.Fatalf("Run returned error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Task closure updated") {
		t.Fatalf("Content = %q, want update confirmation", result.Content)
	}

	snap := state.Snapshot()
	if !snap.Updated {
		t.Fatal("snapshot Updated = false, want true")
	}
	if snap.NeedsCodeChange != ClosureDecisionYes || snap.CodeChangeStatus != ClosureCodeChanged {
		t.Fatalf("code closure = %+v", snap)
	}
	if snap.NeedsVerification != ClosureDecisionYes || snap.VerificationStatus != ClosureVerificationPassed {
		t.Fatalf("verification closure = %+v", snap)
	}
	if len(snap.CodeChangeToolResultRefs) != 1 || snap.CodeChangeToolResultRefs[0] != "call_edit" {
		t.Fatalf("code refs = %+v", snap.CodeChangeToolResultRefs)
	}
	if len(snap.VerificationToolResultRefs) != 1 || snap.VerificationToolResultRefs[0] != "call_test" {
		t.Fatalf("verification refs = %+v", snap.VerificationToolResultRefs)
	}
}

func TestUpdateTaskClosureRejectsInvalidEnums(t *testing.T) {
	state := NewTaskClosureState()
	b, _ := json.Marshal(map[string]any{
		"needs_code_change":   "maybe",
		"code_change_status":  "changed",
		"needs_verification":  "no",
		"verification_status": "not_needed",
	})

	result := NewTaskClosure(state).Run(context.Background(), b, nil)
	if !result.IsError {
		t.Fatal("Run IsError = false, want true")
	}
	if !strings.Contains(result.Content, "invalid needs_code_change") {
		t.Fatalf("Content = %q, want invalid enum message", result.Content)
	}
}
