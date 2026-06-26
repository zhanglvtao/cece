package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHeaderBarToolSummaryGroupsAndAliases(t *testing.T) {
	h := NewHeaderBar()
	h.IncrementTool("ExitPlanMode", true)
	h.IncrementTool("Read", true)
	h.IncrementTool("AskUserQuestion", false)
	h.IncrementTool("Write", true)
	h.IncrementTool("EnterPlanMode", true)
	h.IncrementTool("WebFetch", true)
	h.IncrementTool("Agent", true)
	h.IncrementTool("Compact", true)

	summary := ansi.Strip(h.formatToolGroup())
	wantOrder := []string{
		"Read ✓1",
		"Write ✓1",
		"WebFetch ✓1",
		"Ask ✓0✗1",
		"Compact ✓1",
		"Agent ✓1",
		"EnterPlan ✓1",
		"ExitPlan ✓1",
	}
	last := -1
	for _, want := range wantOrder {
		idx := strings.Index(summary, want)
		if idx < 0 {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
		if idx <= last {
			t.Fatalf("summary order wrong for %q: %s", want, summary)
		}
		last = idx
	}
}

func TestFormatToolTitleKVsUsesDisplayAliases(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "EnterPlanMode", want: "EnterPlan"},
		{name: "ExitPlanMode", want: "ExitPlan"},
		{name: "AskUserQuestion", want: "Ask"},
	}
	for _, tc := range cases {
		raw, _ := json.Marshal(map[string]any{"path": "/tmp/x"})
		got, _ := formatToolTitleKVs(tc.name, raw)
		if got != tc.want {
			t.Fatalf("formatToolTitleKVs(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHeaderBarRenderIncludesCompletionHooksAndRestore(t *testing.T) {
	h := NewHeaderBar()
	h.IncrementAPI(true)
	h.IncrementTurn(true)
	h.IncrementCompletionHook()
	h.IncrementCompletionHook()
	h.UpdateTokens(1200, 3400, 500)

	rendered := ansi.Strip(h.Render(200))
	if !strings.Contains(rendered, "Hook 2") {
		t.Fatalf("render missing completion hook count: %q", rendered)
	}

	h.Restore(3, map[string]int{"Read": 2}, 700, 4, 5)
	restored := ansi.Strip(h.Render(200))
	if !strings.Contains(restored, "Hook 5") {
		t.Fatalf("restored render missing completion hook count: %q", restored)
	}
}
