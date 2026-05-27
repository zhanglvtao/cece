package ui

import "testing"

func TestContextGaugeState(t *testing.T) {
	tests := []struct {
		name      string
		used      int
		window    int
		remaining int
		percent   int
		filled    int
		level     contextGaugeLevel
	}{
		{name: "full", used: 0, window: 200000, remaining: 200000, percent: 100, filled: 10, level: contextGaugeGreen},
		{name: "green", used: 54000, window: 200000, remaining: 146000, percent: 73, filled: 7, level: contextGaugeGreen},
		{name: "yellow", used: 162000, window: 200000, remaining: 38000, percent: 19, filled: 2, level: contextGaugeYellow},
		{name: "red", used: 192000, window: 200000, remaining: 8000, percent: 4, filled: 1, level: contextGaugeRed},
		{name: "empty", used: 200000, window: 200000, remaining: 0, percent: 0, filled: 0, level: contextGaugeEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextGaugeState(tt.used, tt.window)
			if got.remaining != tt.remaining {
				t.Fatalf("remaining = %d, want %d", got.remaining, tt.remaining)
			}
			if got.percent != tt.percent {
				t.Fatalf("percent = %d, want %d", got.percent, tt.percent)
			}
			if got.filled != tt.filled {
				t.Fatalf("filled = %d, want %d", got.filled, tt.filled)
			}
			if got.level != tt.level {
				t.Fatalf("level = %v, want %v", got.level, tt.level)
			}
		})
	}
}

func TestModeLabel(t *testing.T) {
	if got := modeLabel("plan"); got != "Plan" {
		t.Fatalf("modeLabel(plan) = %q, want Plan", got)
	}
	if got := modeLabel("auto-accept"); got != "Auto" {
		t.Fatalf("modeLabel(auto-accept) = %q, want Auto", got)
	}
	if got := modeLabel("default"); got != "Default" {
		t.Fatalf("modeLabel(default) = %q, want Default", got)
	}
	if got := modeLabel(""); got != "Default" {
		t.Fatalf("modeLabel(empty) = %q, want Default", got)
	}
}

func TestFormatTokenK(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want string
	}{
		{name: "zero", in: 0, want: "0K"},
		{name: "under 1K rounds up", in: 999, want: "1K"},
		{name: "exact K", in: 1000, want: "1K"},
		{name: "rounds up", in: 1500, want: "2K"},
		{name: "large", in: 12000, want: "12K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTokenK(tt.in); got != tt.want {
				t.Fatalf("formatTokenK(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStatusLabelShowsSpinnerOnlyWhileRequestingOrStreaming(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")

	m.status = "Ready"
	if got := m.statusLabel(); got != "Ready" {
		t.Fatalf("statusLabel ready = %q, want Ready", got)
	}

	m.status = "Requesting"
	m.statusFrame = 0
	if got, want := m.statusLabel(), "- Requesting"; got != want {
		t.Fatalf("statusLabel requesting = %q, want %q", got, want)
	}

	m.status = "Streaming"
	m.statusFrame = 1
	if got, want := m.statusLabel(), "\\ Streaming"; got != want {
		t.Fatalf("statusLabel streaming = %q, want %q", got, want)
	}
}
