package evals

import (
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/testkit"
)

// Runner is a tiny shared harness wrapper for deterministic eval cases.
type Runner struct {
	T *testing.T
}

func NewRunner(t *testing.T) *Runner {
	t.Helper()
	return &Runner{T: t}
}

// RunWithHarness sends prompt, waits for completion, and evaluates the result.
func (r *Runner) RunWithHarness(c Case, h *testkit.Harness) Outcome {
	r.T.Helper()
	h.Send(c.Prompt)
	testkit.WaitForEvent[protocol.TurnCompleted](r.T, h, nil, 5*time.Second)
	finalAnswer := extractLastAssistantText(h)
	return Evaluate(c, h, finalAnswer)
}

func extractLastAssistantText(h *testkit.Harness) string {
	events := h.EventsSnapshot()
	for i := len(events) - 1; i >= 0; i-- {
		if ev, ok := events[i].(protocol.AssistantDelta); ok {
			text := strings.TrimSpace(ev.Text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}
