package ui

import (
	"strings"
	"testing"
)

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
	if strings.Contains(got, "Ready") {
		t.Fatalf("status should not appear in bottom metrics bar: %q", got)
	}
	if !strings.Contains(got, "sonnet") {
		t.Fatalf("missing model: %q", got)
	}
	if !strings.Contains(got, "calls:1") {
		t.Fatalf("missing calls: %q", got)
	}
	if !strings.Contains(got, "Grep:2") {
		t.Fatalf("missing Grep:2: %q", got)
	}
	if !strings.Contains(got, "Read:1") {
		t.Fatalf("missing Read:1: %q", got)
	}
	if !strings.Contains(got, "in/out:5K") {
		t.Fatalf("missing tokens: %q", got)
	}
	if !strings.Contains(got, "ctx:") {
		t.Fatalf("missing context: %q", got)
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
