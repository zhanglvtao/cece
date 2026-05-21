package prompt

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int // heuristic result, approximate
	}{
		{"empty string", "", 0},
		{"pure ascii", "hello world", 3},    // 11 chars / 4 ≈ 3
		{"long english", repeatStr("a", 400), 100}, // 400 / 4 = 100
		{"chinese text", "你好世界测试", 5},    // 5 chinese chars, ~5/1.5 ≈ 4, but heuristic uses weighted
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.input)
			if tt.want > 0 && got <= 0 {
				t.Errorf("estimateTokens(%q) = %d, want > 0", tt.name, got)
			}
			if tt.input == "" && got != 0 {
				t.Errorf("estimateTokens(empty) = %d, want 0", got)
			}
		})
	}
}

func TestPreciseEstimate(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"simple english", "hello world"},
		{"chinese text", "你好世界"},
		{"mixed content", "hello 你好 world 世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preciseEstimate(tt.input)
			if tt.input == "" && got != 0 {
				t.Errorf("preciseEstimate(empty) = %d, want 0", got)
			}
			if tt.input != "" && got <= 0 {
				t.Errorf("preciseEstimate(%q) = %d, want > 0", tt.input, got)
			}
		})
	}
}

func TestPreciseEstimateAccuracy(t *testing.T) {
	// "hello world" should be 2-3 tokens with cl100k_base
	got := preciseEstimate("hello world")
	if got < 2 || got > 4 {
		t.Errorf("preciseEstimate('hello world') = %d, expected 2-4", got)
	}
}

func TestHeuristicEstimator(t *testing.T) {
	est := heuristicEstimator{}
	got := est.Estimate("test")
	if got != 1 { // 4 chars / 4 = 1
		t.Errorf("heuristicEstimator.Estimate('test') = %d, want 1", got)
	}
}

func TestTiktokenEstimator(t *testing.T) {
	est := tiktokenEstimator{}
	got := est.Estimate("hello world")
	if got <= 0 {
		t.Errorf("tiktokenEstimator.Estimate('hello world') = %d, want > 0", got)
	}
}

// helper
func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
