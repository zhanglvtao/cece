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
	if !strings.Contains(rendered, "▸ res") {
		t.Fatal("expected collapsed res label")
	}
	if !strings.Contains(rendered, "end_turn") {
		t.Fatal("expected stop reason")
	}
	if !strings.Contains(rendered, "42→7") {
		t.Fatal("expected compact token pair")
	}
	if !strings.Contains(rendered, "500.0ms") && !strings.Contains(rendered, "0.5s") {
		t.Fatalf("expected duration, got: %s", rendered)
	}
}

func TestDetailBlockRenderExpandedFields(t *testing.T) {
	d := DetailBlock{
		InputTokens:         100,
		OutputTokens:        10,
		CacheCreationTokens: 1200,
		CacheReadTokens:     800,
		Duration:            1 * time.Second,
		StopReason:          "tool_use",
		ToolCalls:           []string{"Bash", "Edit", "Read"},
		Expanded:            true,
	}
	rendered := d.Render(80, DefaultStyles())
	if !strings.Contains(rendered, "▾ res") {
		t.Fatal("expected expanded res label")
	}
	if !strings.Contains(rendered, "tool_use") {
		t.Fatal("expected stop reason")
	}
	if !strings.Contains(rendered, "100→10") {
		t.Fatal("expected compact token pair")
	}
	if !strings.Contains(rendered, "+1.2k/-800") {
		t.Fatalf("expected compact cache summary, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Bash,Edit+1") {
		t.Fatalf("expected compact calls preview, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Bash, Edit, Read") {
		t.Fatalf("expected expanded full call list, got: %s", rendered)
	}
}

func TestDetailBlockOmitsEmptyFields(t *testing.T) {
	d := DetailBlock{
		InputTokens:  42,
		OutputTokens: 7,
	}
	rendered := d.Render(80, DefaultStyles())
	if strings.Contains(rendered, "+0/-0") {
		t.Fatal("should not show cache when zero")
	}
	if strings.Contains(rendered, "Bash") {
		t.Fatal("should not show calls when empty")
	}
}

func TestRequestDetailRenderCompactSummary(t *testing.T) {
	r := (&requestDetailItem{
		styles:      DefaultStyles(),
		reason:      "tool_result",
		inputTokens: 1200,
		tokensExact: false,
		tools:       []string{"Read", "Edit", "Bash", "Grep"},
		toolResults: []string{"Bash", "Read", "Edit"},
	}).Render(80)
	if !strings.Contains(r, "req") {
		t.Fatal("expected req label")
	}
	if !strings.Contains(r, "tool_result") {
		t.Fatal("expected request reason")
	}
	if !strings.Contains(r, "~1.2k") {
		t.Fatalf("expected compact token summary, got: %s", r)
	}
	if !strings.Contains(r, "Bash,Read+1") {
		t.Fatalf("expected compact tool result preview, got: %s", r)
	}
	if !strings.Contains(r, "Read,Edit+2") {
		t.Fatalf("expected compact tools preview, got: %s", r)
	}
}

func TestCompactNameList(t *testing.T) {
	if got := compactNameList(nil); got != "" {
		t.Fatalf("compactNameList(nil) = %q, want empty", got)
	}
	if got := compactNameList([]string{"Read"}); got != "Read" {
		t.Fatalf("compactNameList(single) = %q", got)
	}
	if got := compactNameList([]string{"Read", "Edit"}); got != "Read,Edit" {
		t.Fatalf("compactNameList(two) = %q", got)
	}
	if got := compactNameList([]string{"Read", "Edit", "Bash", "Grep"}); got != "Read,Edit+2" {
		t.Fatalf("compactNameList(many) = %q", got)
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
