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
