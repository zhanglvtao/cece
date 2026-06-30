package ui

import (
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/logger"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// StatusBar holds data displayed in the bottom status line.
// It renders as a single line: mode | model | effort | ctx | scroll.
type StatusBar struct {
	styles Styles
	mode   string

	modelName     string
	effort        string
	contextUsed   int
	contextWindow int
	scrollPct     int
}

// NewStatusBar creates a new StatusBar.
func NewStatusBar() *StatusBar {
	return &StatusBar{
		styles: DefaultStyles(),
	}
}

// UpdateMode updates the permission mode label.
func (sb *StatusBar) UpdateMode(mode string) { sb.mode = mode }

// UpdateModel updates the model name.
func (sb *StatusBar) UpdateModel(name string) { sb.modelName = name }

// UpdateEffort updates the effort level display.
func (sb *StatusBar) UpdateEffort(effort string) { sb.effort = effort }

// UpdateContext updates the context gauge.
func (sb *StatusBar) UpdateContext(used, window int) {
	if sb.contextWindow != window && window > 0 {
		logger.Info("StatusBar: contextWindow changed", "old", sb.contextWindow, "new", window)
	}
	sb.contextUsed = used
	sb.contextWindow = window
}

// UpdateScroll updates the scroll indicator.
func (sb *StatusBar) UpdateScroll(percent int) { sb.scrollPct = percent }

// Height returns the number of lines (always 1).
func (sb *StatusBar) Height() int { return 1 }

// Render returns a single-line status bar.
func (sb *StatusBar) Render(width int) string {
	var parts []string

	// mode
	parts = append(parts, statusModeStyle(sb.styles, sb.mode).Render(statusModeLabel(sb.mode)))

	// model name
	if sb.modelName != "" {
		parts = append(parts, sb.styles.Status.Model.Render(sb.modelName))
	}

	// effort
	if sb.effort != "" {
		parts = append(parts, sb.styles.Status.Model.Render(sb.effort))
	}

	// context
	if sb.contextWindow > 0 {
		style := contextStyle(sb.styles, sb.contextUsed, sb.contextWindow)
		parts = append(parts, style.Render(formatContextGauge(sb.contextUsed, sb.contextWindow)))
	}

	// scroll
	if sb.scrollPct > 0 {
		parts = append(parts, sb.styles.Status.Scroll.Render(fmt.Sprintf("scroll:%d%%", sb.scrollPct)))
	}

	sep := sb.styles.Status.Separator.Render(" | ")
	line := strings.Join(parts, sep)
	if width > 0 {
		line = ansi.Truncate(line, width, "")
	}
	return line
}

func statusModeLabel(mode string) string {
	label := "Default"
	switch mode {
	case "", "default":
		label = "Default"
	case "auto-accept":
		label = "Auto"
	case "plan":
		label = "Plan"
	default:
		label = strings.ToUpper(mode[:1]) + mode[1:]
	}
	return label
}

func statusModeStyle(styles Styles, mode string) lipgloss.Style {
	switch mode {
	case "auto-accept":
		return styles.Status.ModeAuto
	case "plan":
		return styles.Status.ModePlan
	default:
		return styles.Status.ModeDefault
	}
}

const contextWarningThresholdPct = 20

func contextStyle(styles Styles, used, window int) lipgloss.Style {
	if window > 0 && contextRemainingPct(used, window) < contextWarningThresholdPct {
		return styles.Status.Fail
	}
	return styles.Status.Context
}

func contextRemainingPct(used, window int) int {
	if window <= 0 {
		return 0
	}
	remaining := window - used
	if remaining < 0 {
		remaining = 0
	}
	return remaining * 100 / window
}

func formatContextGauge(used, window int) string {
	remaining := window - used
	if remaining < 0 {
		remaining = 0
	}
	pct := contextRemainingPct(used, window)
	filled := remaining * 10 / window
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	return fmt.Sprintf("%s %s/%s %d%%", bar, formatTokenK(remaining), formatTokenK(window), pct)
}

func formatTokenK(n int) string {
	if n <= 0 {
		return "0K"
	}
	k := (n + 999) / 1000
	return fmt.Sprintf("%dK", k)
}
