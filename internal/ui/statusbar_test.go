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
	sb.UpdateContext(30000, 200000)

	got := sb.Render(120)
	if !strings.Contains(got, "sonnet") {
		t.Fatalf("missing model: %q", got)
	}
	if !strings.Contains(got, "ctx:") {
		t.Fatalf("missing context: %q", got)
	}
}

func TestStatusBarScroll(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")

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