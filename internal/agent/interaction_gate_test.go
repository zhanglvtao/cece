package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestExitPlanModeRequiresApprovalInPlanMode(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewExitPlanMode(nil))

	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	planFile := filepath.Join(planState.PlansDir(), "plan.md")
	if err := os.WriteFile(planFile, []byte("# Plan\n\n- Do it\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	confirmCh := make(chan struct{}, 1)
	gate := NewInteractionGate(registry, planState, false, confirmCh, nil, nil)

	calls := []ApiToolUseBlock{
		{
			ID:    "call_1",
			Name:  tool.ExitPlanModeToolName,
			Input: json.RawMessage(`{"plan_file": "` + planFile + `"}`),
		},
	}

	events := make(chan Event, 16)

	// Run WaitIfNeeded in a goroutine; it should block waiting for approval.
	done := make(chan error, 1)
	go func() {
		done <- gate.WaitIfNeeded(context.Background(), calls, events)
	}()

	select {
	case ev := <-events:
		approval, ok := ev.(PlanApprovalRequested)
		if !ok {
			t.Fatalf("event = %T, want PlanApprovalRequested", ev)
		}
		if approval.PlanFile != "plan.md" || approval.PlanContent == "" {
			t.Fatalf("approval = %#v, want non-empty plan preview", approval)
		}
	case err := <-done:
		t.Fatalf("WaitIfNeeded returned without waiting for approval: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for PlanApprovalRequested")
	}

	// Confirm to unblock.
	confirmCh <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("WaitIfNeeded returned error: %v", err)
	}
}

func TestExitPlanModeWithEmptyPlanDoesNotRequestApproval(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewExitPlanMode(nil))

	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	planFile := filepath.Join(planState.PlansDir(), "empty.md")
	if err := os.WriteFile(planFile, []byte("  \n\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	confirmCh := make(chan struct{}, 1)
	gate := NewInteractionGate(registry, planState, false, confirmCh, nil, nil)
	calls := []ApiToolUseBlock{{
		ID:    "call_1",
		Name:  tool.ExitPlanModeToolName,
		Input: json.RawMessage(`{"plan_file": "` + planFile + `"}`),
	}}
	events := make(chan Event, 16)

	if err := gate.WaitIfNeeded(context.Background(), calls, events); err != nil {
		t.Fatalf("WaitIfNeeded returned error: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected event: %T", ev)
	default:
	}
}

func TestPlanModeAllowedWritesDoNotRequestApproval(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewWrite())

	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	confirmCh := make(chan struct{}, 1)
	gate := NewInteractionGate(registry, planState, false, confirmCh, nil, nil)
	mockupPath := filepath.Join(projectDir, ".superpowers", "brainstorm", "session-1", "content", "mockup.html")
	input, _ := json.Marshal(map[string]string{"path": mockupPath, "content": "mockup"})
	calls := []ApiToolUseBlock{{
		ID:    "call_1",
		Name:  "Write",
		Input: input,
	}}
	events := make(chan Event, 16)

	if err := gate.WaitIfNeeded(context.Background(), calls, events); err != nil {
		t.Fatalf("WaitIfNeeded returned error: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected event: %T", ev)
	default:
	}
}

func TestPlanModeDisallowedWritesSkipApprovalAndLetExecutorReject(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewWrite())

	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	confirmCh := make(chan struct{}, 1)
	gate := NewInteractionGate(registry, planState, false, confirmCh, nil, nil)
	input, _ := json.Marshal(map[string]string{"path": filepath.Join(projectDir, "internal", "x.go"), "content": "package internal"})
	calls := []ApiToolUseBlock{{
		ID:    "call_1",
		Name:  "Write",
		Input: input,
	}}
	events := make(chan Event, 16)

	if err := gate.WaitIfNeeded(context.Background(), calls, events); err != nil {
		t.Fatalf("WaitIfNeeded returned error: %v", err)
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected event: %T", ev)
	default:
	}
}
