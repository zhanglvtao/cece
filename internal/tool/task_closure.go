package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

const TaskClosureToolName = "UpdateTaskClosure"

type ClosureDecision string

const (
	ClosureDecisionYes     ClosureDecision = "yes"
	ClosureDecisionNo      ClosureDecision = "no"
	ClosureDecisionUnknown ClosureDecision = "unknown"
)

type ClosureCodeStatus string

const (
	ClosureCodeChanged   ClosureCodeStatus = "changed"
	ClosureCodeNotNeeded ClosureCodeStatus = "not_needed"
	ClosureCodeBlocked   ClosureCodeStatus = "blocked"
	ClosureCodeUnknown   ClosureCodeStatus = "unknown"
)

type ClosureVerificationStatus string

const (
	ClosureVerificationPassed    ClosureVerificationStatus = "passed"
	ClosureVerificationNotNeeded ClosureVerificationStatus = "not_needed"
	ClosureVerificationNotRun    ClosureVerificationStatus = "not_run"
	ClosureVerificationBlocked   ClosureVerificationStatus = "blocked"
	ClosureVerificationUnknown   ClosureVerificationStatus = "unknown"
)

type TaskClosureSnapshot struct {
	Updated                    bool                      `json:"updated"`
	NeedsCodeChange            ClosureDecision           `json:"needs_code_change"`
	CodeChangeStatus           ClosureCodeStatus         `json:"code_change_status"`
	CodeChangeReason           string                    `json:"code_change_reason"`
	CodeChangeToolResultRefs   []string                  `json:"code_change_tool_result_refs"`
	NeedsVerification          ClosureDecision           `json:"needs_verification"`
	VerificationStatus         ClosureVerificationStatus `json:"verification_status"`
	VerificationReason         string                    `json:"verification_reason"`
	VerificationToolResultRefs []string                  `json:"verification_tool_result_refs"`
	RemainingWork              []string                  `json:"remaining_work"`
}

type TaskClosureState struct {
	mu       sync.Mutex
	snapshot TaskClosureSnapshot
}

func NewTaskClosureState() *TaskClosureState { return &TaskClosureState{} }

func (s *TaskClosureState) Update(snapshot TaskClosureSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot.Updated = true
	s.snapshot = snapshot
}

func (s *TaskClosureState) Snapshot() TaskClosureSnapshot {
	if s == nil {
		return TaskClosureSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snapshot
	out.CodeChangeToolResultRefs = append([]string(nil), out.CodeChangeToolResultRefs...)
	out.VerificationToolResultRefs = append([]string(nil), out.VerificationToolResultRefs...)
	out.RemainingWork = append([]string(nil), out.RemainingWork...)
	return out
}

func (s *TaskClosureState) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = TaskClosureSnapshot{}
}

type taskClosureTool struct {
	state *TaskClosureState
}

func NewTaskClosure(state *TaskClosureState) Tool { return taskClosureTool{state: state} }

func (taskClosureTool) Effect() Effect { return EffectMode }

func (taskClosureTool) Info() Definition {
	return Definition{
		Name:        TaskClosureToolName,
		Description: "Declare whether the current task is ready to finish. Use before ending implementation tasks. Reference code change or verification tool results by their tool_use_id in *_tool_result_refs; do not invent ids.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"needs_code_change", "code_change_status", "needs_verification", "verification_status", "remaining_work"},
			"properties": map[string]any{
				"needs_code_change":             map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}},
				"code_change_status":            map[string]any{"type": "string", "enum": []string{"changed", "not_needed", "blocked", "unknown"}},
				"code_change_reason":            map[string]any{"type": "string"},
				"code_change_tool_result_refs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"needs_verification":            map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}},
				"verification_status":           map[string]any{"type": "string", "enum": []string{"passed", "not_needed", "not_run", "blocked", "unknown"}},
				"verification_reason":           map[string]any{"type": "string"},
				"verification_tool_result_refs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"remaining_work":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
	}
}

func (t taskClosureTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.state == nil {
		return Result{Content: "task closure state is not configured", IsError: true}
	}
	var parsed TaskClosureSnapshot
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}
	}
	if err := validateTaskClosureSnapshot(parsed); err != nil {
		return Result{Content: err.Error(), IsError: true}
	}
	t.state.Update(parsed)
	return Result{Content: "Task closure updated."}
}

func validateTaskClosureSnapshot(s TaskClosureSnapshot) error {
	if !validClosureDecision(s.NeedsCodeChange) {
		return fmt.Errorf("invalid needs_code_change %q", s.NeedsCodeChange)
	}
	if !validCodeStatus(s.CodeChangeStatus) {
		return fmt.Errorf("invalid code_change_status %q", s.CodeChangeStatus)
	}
	if !validClosureDecision(s.NeedsVerification) {
		return fmt.Errorf("invalid needs_verification %q", s.NeedsVerification)
	}
	if !validVerificationStatus(s.VerificationStatus) {
		return fmt.Errorf("invalid verification_status %q", s.VerificationStatus)
	}
	return nil
}

func validClosureDecision(v ClosureDecision) bool {
	switch v {
	case ClosureDecisionYes, ClosureDecisionNo, ClosureDecisionUnknown:
		return true
	default:
		return false
	}
}

func validCodeStatus(v ClosureCodeStatus) bool {
	switch v {
	case ClosureCodeChanged, ClosureCodeNotNeeded, ClosureCodeBlocked, ClosureCodeUnknown:
		return true
	default:
		return false
	}
}

func validVerificationStatus(v ClosureVerificationStatus) bool {
	switch v {
	case ClosureVerificationPassed, ClosureVerificationNotNeeded, ClosureVerificationNotRun, ClosureVerificationBlocked, ClosureVerificationUnknown:
		return true
	default:
		return false
	}
}
