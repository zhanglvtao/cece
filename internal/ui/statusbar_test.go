package ui

import (
	"strings"
	"testing"
)

func TestStatusBarModeFirstColumn(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateMode("plan")
	sb.UpdateModel("sonnet")

	got := stripAnsi(sb.Render(120))
	parts := strings.Split(got, " | ")
	if len(parts) < 2 {
		t.Fatalf("statusbar parts = %v, want at least mode and model", parts)
	}
	if parts[0] != "plan ✎" {
		t.Fatalf("first column = %q, want %q", parts[0], "plan ✎")
	}
	if parts[1] != "sonnet" {
		t.Fatalf("second column = %q, want model", parts[1])
	}
}

func TestStatusBarModeSymbols(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "", want: "default ○"},
		{mode: "default", want: "default ○"},
		{mode: "auto-accept", want: "auto-accept ✓"},
		{mode: "plan", want: "plan ✎"},
		{mode: "unknown", want: "unknown ○"},
	}
	for _, tt := range tests {
		sb := NewStatusBar()
		sb.UpdateMode(tt.mode)
		got := stripAnsi(sb.Render(120))
		parts := strings.Split(got, " | ")
		if parts[0] != tt.want {
			t.Fatalf("mode %q rendered %q, want %q", tt.mode, parts[0], tt.want)
		}
	}
}

func TestStatusBarRender(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")
	sb.UpdateStatus("Ready", false)
	sb.IncrementAPICalls()
	sb.IncrementTool("Grep")
	sb.IncrementTool("Read")
	sb.IncrementTool("Grep")
	sb.UpdateTokens(5000, 2000)
	sb.UpdateContext(30000, 200000)

	got := sb.Render(120)
	lines := strings.Split(got, "\n")

	// Line 1: no tool info
	if strings.Contains(lines[0], "api:") {
		t.Fatalf("line 1 should not contain tool info: %q", lines[0])
	}
	if !strings.Contains(lines[0], "sonnet") {
		t.Fatalf("missing model in line 1: %q", lines[0])
	}
	if !strings.Contains(lines[0], "in/out/cache:5K") {
		t.Fatalf("missing tokens in line 1: %q", lines[0])
	}
	if !strings.Contains(lines[0], "ctx:") {
		t.Fatalf("missing context in line 1: %q", lines[0])
	}

	// Line 2: compact tool info
	if len(lines) < 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "api:1") {
		t.Fatalf("missing api:1 in line 2: %q", lines[1])
	}
	if !strings.Contains(lines[1], "Grep×2") {
		t.Fatalf("missing Grep×2 in line 2: %q", lines[1])
	}
	if !strings.Contains(lines[1], "Read×1") {
		t.Fatalf("missing Read×1 in line 2: %q", lines[1])
	}

	// Verify short names for long tool names
	sb2 := NewStatusBar()
	sb2.IncrementTool("EnterPlanMode")
	sb2.IncrementTool("AskUserQuestion")
	sb2.IncrementTool("WebFetch")
	sb2.IncrementTool("Compact")
	got2 := stripAnsi(sb2.Render(120))
	if !strings.Contains(got2, "Plan×1") {
		t.Fatalf("missing Plan×1 for EnterPlanMode: %q", got2)
	}
	if !strings.Contains(got2, "Ask×1") {
		t.Fatalf("missing Ask×1 for AskUserQuestion: %q", got2)
	}
	if !strings.Contains(got2, "Web×1") {
		t.Fatalf("missing Web×1 for WebFetch: %q", got2)
	}
	if !strings.Contains(got2, "Cmpct×1") {
		t.Fatalf("missing Cmpct×1 for Compact: %q", got2)
	}

	// Verify MCP tool name shortening
	sb3 := NewStatusBar()
	sb3.IncrementTool("mcp_github_search_repositories")
	sb3.IncrementTool("mcp_github_get_file")
	sb3.IncrementTool("mcp_slack_send_message")
	got3 := stripAnsi(sb3.Render(120))
	if !strings.Contains(got3, "github/search_repositories×1") {
		t.Fatalf("missing shortened MCP tool name: %q", got3)
	}
	if !strings.Contains(got3, "github/get_file×1") {
		t.Fatalf("missing shortened MCP tool name: %q", got3)
	}
	if !strings.Contains(got3, "slack/send_message×1") {
		t.Fatalf("missing shortened MCP tool name: %q", got3)
	}
}

func TestStatusBarCacheHitRate(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")
	sb.UpdateTokens(10000, 2000)
	sb.UpdateCache(8000, 2000)

	got := sb.Render(120)
	if !strings.Contains(got, "in/out/cache:10K/2K/8K") {
		t.Fatalf("missing cache read tokens: %q", got)
	}
	if !strings.Contains(got, " 80%") {
		t.Fatalf("missing cache hit rate: %q", got)
	}
}

func TestStatusBarScroll(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")
	sb.UpdateStatus("Ready", false)

	got := sb.Render(80)
	if strings.Contains(got, "scroll:") {
		t.Fatalf("scroll should not appear when 0: %q", got)
	}

	sb.UpdateScroll(42)
	got = sb.Render(80)
	if !strings.Contains(got, "scroll:42%") {
		t.Fatalf("missing scroll: %q", got)
	}
}

func TestStatusBarReset(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")
	sb.IncrementAPICalls()
	sb.IncrementTool("Grep")
	sb.ResetToolCounts()

	if sb.apiCalls != 0 {
		t.Fatalf("apiCalls = %d, want 0", sb.apiCalls)
	}
	if len(sb.toolCounts) != 0 {
		t.Fatalf("toolCounts should be empty after reset")
	}
}

func TestStatusBarBusy(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateStatus("Streaming", true)
	got := sb.Render(80)
	// Bottom metrics bar no longer includes status text
	if strings.Contains(got, "Streaming") {
		t.Fatalf("bottom bar should not contain status: %q", got)
	}
}

func TestFormatTokenK(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0K"},
		{999, "1K"},
		{1000, "1K"},
		{1500, "2K"},
		{12000, "12K"},
	}
	for _, tt := range tests {
		if got := formatTokenK(tt.in); got != tt.want {
			t.Fatalf("formatTokenK(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
