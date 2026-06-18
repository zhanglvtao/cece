package ui

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/ui/theme"
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
	if parts[0] != "  Plan" {
		t.Fatalf("first column = %q, want %q", parts[0], "  Plan")
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
		{mode: "", want: "  Default"},
		{mode: "default", want: "  Default"},
		{mode: "auto-accept", want: "  Auto"},
		{mode: "plan", want: "  Plan"},
		{mode: "unknown", want: "  Unknown"},
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

func TestStatusBarModeStyles(t *testing.T) {
	styles := DefaultStyles()
	cases := []struct {
		mode string
		want any
	}{
		{mode: "", want: theme.FgSubtle},
		{mode: "default", want: theme.FgSubtle},
		{mode: "plan", want: theme.Blue},
		{mode: "auto-accept", want: theme.Green},
		{mode: "unknown", want: theme.FgSubtle},
	}
	for _, tt := range cases {
		if got := statusModeStyle(styles, tt.mode).GetForeground(); got != tt.want {
			t.Fatalf("mode %q foreground = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestStatusBarRender(t *testing.T) {
	sb := NewStatusBar()
	sb.UpdateModel("sonnet")
	sb.UpdateContext(30000, 200000)

	got := stripAnsi(sb.Render(120))
	if !strings.Contains(got, "sonnet") {
		t.Fatalf("missing model: %q", got)
	}
	if !strings.Contains(got, "████████░░ 170K/200K 85%") {
		t.Fatalf("missing context gauge: %q", got)
	}
	if strings.Contains(got, "ctx:") {
		t.Fatalf("old context label should not appear: %q", got)
	}
}

func TestFormatContextGauge(t *testing.T) {
	tests := []struct {
		name   string
		used   int
		window int
		want   string
	}{
		{name: "full", used: 0, window: 270000, want: "██████████ 270K/270K 100%"},
		{name: "sixty", used: 108000, window: 270000, want: "██████░░░░ 162K/270K 60%"},
		{name: "empty", used: 300000, window: 270000, want: "░░░░░░░░░░ 0K/270K 0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatContextGauge(tt.used, tt.window); got != tt.want {
				t.Fatalf("formatContextGauge(%d, %d) = %q, want %q", tt.used, tt.window, got, tt.want)
			}
		})
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
