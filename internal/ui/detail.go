package ui

import (
	"fmt"
	"strings"
	"time"
)

// formatTokenCount renders token counts compactly: values > 1000 use "1.2k" format.
func formatTokenCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func compactNameList(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) <= 2 {
		return strings.Join(names, ",")
	}
	return strings.Join(names[:2], ",") + fmt.Sprintf("+%d", len(names)-2)
}

// DetailBlock holds response metadata for one assistant turn.
type DetailBlock struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	Duration            time.Duration
	StopReason          string
	ToolCalls           []string
	Expanded            bool
}

// Render produces the detail string for display below the assistant message.
func (d DetailBlock) Render(width int, sty Styles) string {
	label := sty.Chat.ResponseLabel.Render("▾ res")
	if !d.Expanded {
		label = sty.Chat.ResponseLabel.Render("▸ res")
	}

	parts := []string{label}
	if d.StopReason != "" {
		parts = append(parts, d.StopReason)
	}
	parts = append(parts, fmt.Sprintf("%s→%s", formatTokenCount(d.InputTokens), formatTokenCount(d.OutputTokens)))
	if d.Duration > 0 {
		parts = append(parts, formatDuration(d.Duration))
	}
	if d.CacheCreationTokens > 0 || d.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("+%s/-%s", formatTokenCount(d.CacheCreationTokens), formatTokenCount(d.CacheReadTokens)))
	}
	if preview := compactNameList(d.ToolCalls); preview != "" {
		parts = append(parts, preview)
	}

	lines := []string{"  " + strings.Join(parts, " · ")}
	if d.Expanded && len(d.ToolCalls) > 2 {
		lines = append(lines, "    "+strings.Join(d.ToolCalls, ", "))
	}
	return sty.Detail.Render(strings.Join(lines, "\n"))
}
