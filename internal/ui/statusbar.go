package ui

import (
	"fmt"
	"strings"

	"github.com/zhanglvtao/cece/internal/logger"

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
	parts = append(parts, sb.styles.Status.Model.Render(statusModeLabel(sb.mode)))

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
		remaining := sb.contextWindow - sb.contextUsed
		if remaining < 0 {
			remaining = 0
		}
		pct := remaining * 100 / sb.contextWindow
		parts = append(parts, sb.styles.Status.Context.Render(fmt.Sprintf("ctx:%s/%s %d%%", formatTokenK(remaining), formatTokenK(sb.contextWindow), pct)))
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
	if mode == "" {
		mode = "default"
	}
	symbol := "○"
	switch mode {
	case "auto-accept":
		symbol = "✓"
	case "plan":
		symbol = "✎"
	}
	return fmt.Sprintf("%s %s", mode, symbol)
}

func formatTokenK(n int) string {
	if n <= 0 {
		return "0K"
	}
	k := (n + 999) / 1000
	return fmt.Sprintf("%dK", k)
}