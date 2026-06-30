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
	if err := os.WriteFile(planFile, []byte(completePlanForApprovalTest()), 0o644); err != nil {
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

func TestExitPlanModeWithLowQualityPlanDoesNotRequestApproval(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewExitPlanMode(nil))

	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	planFile := filepath.Join(planState.PlansDir(), "low-quality.md")
	if err := os.WriteFile(planFile, []byte("# Plan\n\n- Do it\n"), 0o644); err != nil {
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

func completePlanForApprovalTest() string {
	return `# Complete Plan

## Context
This plan improves plan approval so users only review implementation plans that are complete enough to execute.

## File Structure
- internal/tool/plan_mode.go: owns the shared ExitPlanMode readiness validation contract.
- internal/agent/interaction_gate.go: calls the shared validation contract before requesting approval.

## Reuse
- Reuse the existing PlanApprovalRequested event so no additional tool or UI flow is needed.
- Reuse PlanModeState.PlansDir to resolve relative plan_file inputs.

## Implementation Tasks
1. Validate plan content before ExitPlanMode changes permission mode.
2. Validate the same content before emitting PlanApprovalRequested.
3. Keep incomplete plans in the normal tool-result correction flow.

## Verification
Run go test ./internal/tool ./internal/agent -count=1 and confirm skeleton plans do not trigger approval.

## Risks
The keyword validator can reject unusual but valid plans, so the rule stays limited to obvious missing sections and placeholders.

## Non-goals
This does not create a second ExitPlanMode tool, add a planner agent, or replace user approval.
`
}

func TestPlanModeAllowedWritesDoNotRequestApproval(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewWrite(nil))

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
	registry.Register(tool.NewWrite(nil))

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
