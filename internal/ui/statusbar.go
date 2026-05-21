package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// contextGaugeLevel is the visual severity for remaining context.
type contextGaugeLevel int

const (
	contextGaugeEmpty contextGaugeLevel = iota
	contextGaugeGreen
	contextGaugeYellow
	contextGaugeRed
)

const contextGaugeCells = 10

type contextGauge struct {
	remaining int
	percent   int
	filled    int
	level     contextGaugeLevel
}

func contextGaugeState(used, window int) contextGauge {
	if window <= 0 {
		return contextGauge{level: contextGaugeEmpty}
	}

	remaining := window - used
	if remaining < 0 {
		remaining = 0
	}
	if remaining > window {
		remaining = window
	}

	percent := remaining * 100 / window
	filled := (percent*contextGaugeCells + 50) / 100 // round to nearest cell
	if percent > 0 && filled == 0 {
		filled = 1
	}
	if filled > contextGaugeCells {
		filled = contextGaugeCells
	}

	level := contextGaugeEmpty
	switch {
	case percent >= 20:
		level = contextGaugeGreen
	case percent >= 5:
		level = contextGaugeYellow
	case percent > 0:
		level = contextGaugeRed
	}

	return contextGauge{
		remaining: remaining,
		percent:   percent,
		filled:    filled,
		level:     level,
	}
}

func renderContextGauge(sty Styles, used, window int) string {
	state := contextGaugeState(used, window)
	if window <= 0 {
		return ""
	}

	levelStyle := sty.StatusBar.ContextEmpty
	switch state.level {
	case contextGaugeGreen:
		levelStyle = sty.StatusBar.ContextGood
	case contextGaugeYellow:
		levelStyle = sty.StatusBar.ContextWarn
	case contextGaugeRed:
		levelStyle = sty.StatusBar.ContextDanger
	}

	filled := strings.Repeat("█", state.filled)
	empty := strings.Repeat("░", contextGaugeCells-state.filled)
	bar := levelStyle.Render(filled) + sty.StatusBar.ContextEmpty.Render(empty)
	percent := levelStyle.Render(fmt.Sprintf("%d%%", state.percent))
	remaining := levelStyle.Render(formatTokenK(state.remaining))
	total := sty.StatusBar.ContextInfo.Render("/" + formatTokenK(window))

	return fmt.Sprintf("%s %s %s%s", bar, percent, remaining, total)
}

func formatTokenK(n int) string {
	if n <= 0 {
		return "0K"
	}
	return fmt.Sprintf("%dK", (n+999)/1000)
}

// StatusBarData holds the dynamic data displayed in the status line.
type StatusBarData struct {
	Status        string
	Model         string
	GitBranch     string
	WorkDir       string
	InputTokens   int
	OutputTokens  int
	ContextUsed   int
	ContextWindow int
	Busy          bool
}

// drawStatusBar renders the 1-line status bar with structured sections.
//
// Layout:
//
//	[● Ready]  model-name  │  main  cece  │  2.1k/200k  in:42 out:7     enter·send  esc·quit
//	 ─status──  ──model──   ──project────   ────context/tokens────────   ────key hints────
func drawStatusBar(scr uv.Screen, area uv.Rectangle, sty Styles, data StatusBarData) {
	var b strings.Builder

	// Section 1: Status indicator (always visible)
	sep := sty.StatusBar.Separator.Render(" │ ")
	if data.Busy {
		b.WriteString(sty.StatusBar.PillActive.Render("● " + data.Status))
	} else {
		b.WriteString(sty.StatusBar.Pill.Render("○ " + data.Status))
	}

	// Section 2: Model name (text only, Info colored)
	if data.Model != "" {
		b.WriteString(" ")
		b.WriteString(sty.StatusBar.Model.Render(data.Model))
	}

	// Section 3: Project info (git branch + workdir)
	projectParts := []string{}
	if data.GitBranch != "" {
		projectParts = append(projectParts, data.GitBranch)
	}
	if data.WorkDir != "" {
		projectParts = append(projectParts, data.WorkDir)
	}
	if len(projectParts) > 0 {
		b.WriteString(" ")
		b.WriteString(sep)
		b.WriteString(" ")
		b.WriteString(sty.StatusBar.Project.Render(strings.Join(projectParts, "  ")))
	}

	// Section 4: Context & token usage
	contextParts := []string{}
	if data.ContextWindow > 0 {
		contextParts = append(contextParts, renderContextGauge(sty, data.ContextUsed, data.ContextWindow))
	}
	if data.InputTokens > 0 || data.OutputTokens > 0 {
		contextParts = append(contextParts, fmt.Sprintf("in:%d out:%d", data.InputTokens, data.OutputTokens))
	}
	if len(contextParts) > 0 {
		b.WriteString(" ")
		b.WriteString(sep)
		b.WriteString(" ")
		b.WriteString(sty.StatusBar.ContextInfo.Render(strings.Join(contextParts, "  ")))
	}

	// Right-aligned key hints
	hints := sty.StatusBar.KeyHint.Render("enter·send  esc·quit  ctrl+o·focus  ctrl+s·sessions")
	line := b.String()
	content := padRight(line, hints, area.Dx())

	uv.NewStyledString(content).Draw(scr, area)
}

// padRight pads the left string and appends the right string, aligning it
// to the right edge of the given width.
func padRight(left, right string, width int) string {
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap <= 0 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
