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
	label := sty.Chat.ResponseLabel.Render("◆ Response")

	var parts []string
	parts = append(parts, label)

	parts = append(parts, fmt.Sprintf("in:%s out:%s", formatTokenCount(d.InputTokens), formatTokenCount(d.OutputTokens)))

	if d.CacheCreationTokens > 0 || d.CacheReadTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache:+%s/−%s", formatTokenCount(d.CacheCreationTokens), formatTokenCount(d.CacheReadTokens)))
	}

	if d.Duration > 0 {
		parts = append(parts, formatDuration(d.Duration))
	}

	if d.StopReason != "" {
		parts = append(parts, fmt.Sprintf("stop:%s", d.StopReason))
	}

	if len(d.ToolCalls) > 0 {
		parts = append(parts, fmt.Sprintf("calls:%s", strings.Join(d.ToolCalls, "·")))
	}

	line := "  " + strings.Join(parts, "  ")
	return sty.Detail.Render(line)
}
