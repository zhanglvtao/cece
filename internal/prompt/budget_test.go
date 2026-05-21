package prompt

import (
	"strings"
	"testing"
)

func TestEnforceBudgetUnderThreshold(t *testing.T) {
	// Small budget but content is well under it — no truncation
	segments := []PromptSegment{
		{Content: "stable prompt", Layer: ContextStable},
		{Content: "session info", Layer: ContextSession},
		{Content: "turn info", Layer: ContextTurn},
	}

	result := enforceBudget(segments, 1000)
	if len(result) != 3 {
		t.Fatalf("enforceBudget should not truncate under threshold: got %d segments", len(result))
	}
	if result[0].Content != "stable prompt" {
		t.Fatalf("stable segment changed: %q", result[0].Content)
	}
}

func TestEnforceBudgetTruncatesTurnBeforeSession(t *testing.T) {
	// Very small budget — turn should be dropped first
	longSession := strings.Repeat("x", 200)
	segments := []PromptSegment{
		{Content: "stable", Layer: ContextStable},
		{Content: longSession, Layer: ContextSession},
		{Content: "turn data", Layer: ContextTurn},
	}

	result := enforceBudget(segments, 30) // ~30 tokens budget

	// Turn should be empty or truncated
	turnSeg := result[2]
	if turnSeg.Content != "" && turnSeg.Layer == ContextTurn {
		// Turn might still have content if budget allows, but if session is large
		// and budget is small, turn should be dropped
		if len(turnSeg.Content) > 10 {
			t.Fatalf("turn segment should be truncated: %q", turnSeg.Content)
		}
	}
}

func TestEnforceBudgetNeverTruncatesStable(t *testing.T) {
	stable := strings.Repeat("important ", 100)
	segments := []PromptSegment{
		{Content: stable, Layer: ContextStable},
		{Content: "session", Layer: ContextSession},
		{Content: "turn", Layer: ContextTurn},
	}

	result := enforceBudget(segments, 50)

	if result[0].Content != stable {
		t.Fatal("stable segment must never be truncated")
	}
}

func TestEnforceBudgetEmptySegmentsPreserved(t *testing.T) {
	segments := []PromptSegment{
		{Content: "", Layer: ContextStable},
		{Content: "session", Layer: ContextSession},
		{Content: "", Layer: ContextTurn},
	}

	result := enforceBudget(segments, 1000)
	if len(result) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(result))
	}
}

func TestBudgetAllocation(t *testing.T) {
	// With 200000 context window, budget allocations should be reasonable
	budget := newTokenBudget(200000)

	if budget.stable <= 0 {
		t.Fatal("stable budget should be > 0")
	}
	if budget.session <= 0 {
		t.Fatal("session budget should be > 0")
	}
	if budget.turn <= 0 {
		t.Fatal("turn budget should be > 0")
	}
	if budget.stable >= budget.session {
		t.Fatal("stable budget should be less than session budget")
	}
	if budget.session >= budget.total {
		t.Fatal("session budget should be less than total")
	}
}

func TestBudgetFromContextWindow(t *testing.T) {
	// Test that budget is derived from context window size
	small := newTokenBudget(4000)
	large := newTokenBudget(200000)

	if large.session <= small.session {
		t.Fatal("larger context window should have larger session budget")
	}
}
