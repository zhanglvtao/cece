package ui

import (
	"strings"
	"testing"
	"time"
)

func TestDetailBlockRenderBasicFields(t *testing.T) {
	d := DetailBlock{
		InputTokens:  42,
		OutputTokens: 7,
		Duration:     500 * time.Millisecond,
		StopReason:   "end_turn",
	}
	rendered := d.Render(80, DefaultStyles())
	if !strings.Contains(rendered, "◆ Response") {
		t.Fatal("expected ◆ Response label")
	}
	if !strings.Contains(rendered, "in:42") {
		t.Fatal("expected in:42")
	}
	if !strings.Contains(rendered, "out:7") {
		t.Fatal("expected out:7")
	}
	if !strings.Contains(rendered, "500.0ms") && !strings.Contains(rendered, "0.5s") {
		t.Fatalf("expected duration, got: %s", rendered)
	}
	if !strings.Contains(rendered, "stop:end_turn") {
		t.Fatal("expected stop:end_turn")
	}
}

func TestDetailBlockRenderCacheTokens(t *testing.T) {
	d := DetailBlock{
		InputTokens:         100,
		OutputTokens:        10,
		CacheCreationTokens: 1200,
		CacheReadTokens:     800,
		Duration:            1 * time.Second,
		StopReason:          "end_turn",
	}
	rendered := d.Render(80, DefaultStyles())
	if !strings.Contains(rendered, "cache:+1.2k") {
		t.Fatalf("expected cache:+1.2k, got: %s", rendered)
	}
	if !strings.Contains(rendered, "−800") {
		t.Fatalf("expected −800, got: %s", rendered)
	}
}

func TestDetailBlockRenderToolCalls(t *testing.T) {
	d := DetailBlock{
		InputTokens:  50,
		OutputTokens: 20,
		StopReason:   "tool_use",
		ToolCalls:    []string{"Bash", "Edit"},
	}
	rendered := d.Render(80, DefaultStyles())
	if !strings.Contains(rendered, "calls:Bash·Edit") {
		t.Fatalf("expected calls:Bash·Edit, got: %s", rendered)
	}
}

func TestDetailBlockOmitsEmptyFields(t *testing.T) {
	d := DetailBlock{
		InputTokens:  42,
		OutputTokens: 7,
	}
	rendered := d.Render(80, DefaultStyles())
	if strings.Contains(rendered, "cache:") {
		t.Fatal("should not show cache when zero")
	}
	if strings.Contains(rendered, "stop:") {
		t.Fatal("should not show stop when empty")
	}
	if strings.Contains(rendered, "calls:") {
		t.Fatal("should not show calls when empty")
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1200, "1.2k"},
		{10000, "10.0k"},
	}
	for _, tt := range tests {
		got := formatTokenCount(tt.input)
		if got != tt.want {
			t.Errorf("formatTokenCount(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
