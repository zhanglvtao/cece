package evals

import (
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/evals/recording"
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

// RunWithReplay creates a harness from a recorded cassette and runs the case.
func (r *Runner) RunWithReplay(c Case, cassettePath string, opts ...testkit.HarnessOption) Outcome {
	r.T.Helper()
	cassette, err := recording.Load(cassettePath)
	if err != nil {
		r.T.Fatalf("load cassette %s: %v", cassettePath, err)
	}

	// Convert cassette turns to ScriptedTurns for the primary LLM
	turns := cassetteToScriptedTurns(cassette)
	llm := testkit.NewScriptedClient(turns...)

	h := testkit.NewHarness(r.T, llm, opts...)
	return r.RunWithHarness(c, h)
}

func cassetteToScriptedTurns(c *recording.Cassette) []testkit.ScriptedTurn {
	turns := make([]testkit.ScriptedTurn, len(c.Turns))
	for i, ct := range c.Turns {
		events := make([]agent.ApiStreamEvent, len(ct.Events))
		for j, ce := range ct.Events {
			events[j] = ce.ToApiEvent()
		}
		turns[i] = testkit.ScriptedTurn{Events: events}
	}
	return turns
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
