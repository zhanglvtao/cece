package ui

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/protocol"
)

func TestTranscriptRendersCompletionGateSummary(t *testing.T) {
	tr := newTranscript()
	tr.apply(protocol.CompletionGateEvaluated{
		Attempt:     1,
		MaxAttempts: 3,
		Status:      protocol.CompletionGateBlocked,
		Next:        "continue",
		Checks: []protocol.CompletionGateCheck{
			{Name: "PlanModeGate", Status: protocol.CompletionGatePassed},
			{Name: "TodoGate", Status: protocol.CompletionGateBlocked, Details: []string{"task \"Run tests\" is still in_progress."}},
		},
	})

	plain := stripAnsi(tr.render(100, DefaultStyles()))
	if !strings.Contains(plain, "Completion gate") || !strings.Contains(plain, "hook 1/3") || !strings.Contains(plain, "Todo ✗") || !strings.Contains(plain, "→ continue") {
		t.Fatalf("rendered gate summary missing expected parts:\n%s", plain)
	}
	if !strings.Contains(plain, "TodoGate: task \"Run tests\" is still in_progress.") {
		t.Fatalf("rendered gate details missing:\n%s", plain)
	}
}

func TestTranscriptLimitsCompletionGateDetails(t *testing.T) {
	tr := newTranscript()
	tr.apply(protocol.CompletionGateEvaluated{
		Attempt:     2,
		MaxAttempts: 3,
		Status:      protocol.CompletionGateBlocked,
		Next:        "continue",
		Checks: []protocol.CompletionGateCheck{
			{Name: "PlanModeGate", Status: protocol.CompletionGateBlocked, Details: []string{"plan still active", "ask or exit"}},
			{Name: "TodoGate", Status: protocol.CompletionGateBlocked, Details: []string{"todo one", "todo two", "todo three"}},
		},
	})

	plain := stripAnsi(tr.render(100, DefaultStyles()))
	lines := strings.Split(strings.TrimSpace(plain), "\n")
	if len(lines) > 5 {
		t.Fatalf("completion gate rendered %d lines, want <= 5:\n%s", len(lines), plain)
	}
	if strings.Contains(plain, "todo three") {
		t.Fatalf("details were not capped:\n%s", plain)
	}
}
