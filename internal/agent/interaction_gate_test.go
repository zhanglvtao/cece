package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestExitPlanModeRequiresApprovalInPlanMode(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewExitPlanMode(nil))

	planState := tool.NewPlanModeState()
	planState.SetProjectDir(t.TempDir())
	planState.Enter()

	confirmCh := make(chan struct{}, 1)
	gate := NewInteractionGate(registry, planState, false, confirmCh, nil, nil)

	calls := []ApiToolUseBlock{
		{
			ID:   "call_1",
			Name: tool.ExitPlanModeToolName,
			Input: json.RawMessage(`{"plan_file": "/tmp/plan.md"}`),
		},
	}

	events := make(chan Event, 16)

	// Run WaitIfNeeded in a goroutine; it should block waiting for approval.
	done := make(chan error, 1)
	go func() {
		done <- gate.WaitIfNeeded(context.Background(), calls, events)
	}()

	// Wait for the PlanApprovalRequested event.
	var gotApproval bool
	for {
		select {
		case ev := <-events:
			if _, ok := ev.(PlanApprovalRequested); ok {
				gotApproval = true
			}
		case <-done:
			// WaitIfNeeded returned without blocking — auto-approved!
			t.Fatal("WaitIfNeeded returned without waiting for approval")
		default:
			if gotApproval {
				break
			}
		}
		if gotApproval {
			break
		}
	}

	// Confirm to unblock.
	confirmCh <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("WaitIfNeeded returned error: %v", err)
	}
}
