package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// StatusBar holds all data displayed in the bottom status line.
// It renders as plain text with "|" separators.
type StatusBar struct {
	styles Styles
	// data
	mode                string
	modelName           string
	status              string
	busy                bool
	spinnerActive       bool // true when status ends with "ing" — spinner animation
	statusFrame         int
	apiCalls            int
	toolCounts          map[string]int
	inputTokens         int
	outputTokens        int
	contextUsed         int
	contextWindow       int
	scrollPct           int
	cacheReadTokens     int
	cacheCreationTokens int
}

var statusSpinnerFrames = []rune{'-', '\\', '|', '/'}

// NewStatusBar creates a new StatusBar.
func NewStatusBar() *StatusBar {
	return &StatusBar{
		styles:     DefaultStyles(),
		toolCounts: make(map[string]int),
	}
}

// UpdateMode updates the permission mode label.
func (sb *StatusBar) UpdateMode(mode string) { sb.mode = mode }

// UpdateModel updates the model name.
func (sb *StatusBar) UpdateModel(name string) { sb.modelName = name }

// UpdateStatus updates the status text and busy flag.
// Sets spinnerActive when status ends with "ing".
func (sb *StatusBar) UpdateStatus(status string, busy bool) {
	sb.status = status
	sb.busy = busy
	sb.spinnerActive = strings.HasSuffix(status, "ing")
}

// TickStatusSpinner advances the spinner frame.
func (sb *StatusBar) TickStatusSpinner() { sb.statusFrame++ }

// IncrementAPICalls increments the API call counter.
func (sb *StatusBar) IncrementAPICalls() { sb.apiCalls++ }

// IncrementTool increments the tool count for the given tool name.
func (sb *StatusBar) IncrementTool(name string) { sb.toolCounts[name]++ }

// UpdateTokens updates token usage.
func (sb *StatusBar) UpdateTokens(input, output int) {
	sb.inputTokens = input
	sb.outputTokens = output
}

// UpdateCache updates cumulative cache token data.
func (sb *StatusBar) UpdateCache(read, creation int) {
	sb.cacheReadTokens = read
	sb.cacheCreationTokens = creation
}

// UpdateContext updates the context gauge.
func (sb *StatusBar) UpdateContext(used, window int) {
	sb.contextUsed = used
	sb.contextWindow = window
}

// UpdateScroll updates the scroll indicator.
func (sb *StatusBar) UpdateScroll(percent int) { sb.scrollPct = percent }

// ResetToolCounts clears all tool counts and API calls.
func (sb *StatusBar) ResetToolCounts() {
	for k := range sb.toolCounts {
		delete(sb.toolCounts, k)
	}
	sb.apiCalls = 0
}

// SetAPICalls sets the API call counter to the given value.
func (sb *StatusBar) SetAPICalls(n int) { sb.apiCalls = n }

// SetToolCounts replaces the tool counts map with the given one.
func (sb *StatusBar) SetToolCounts(m map[string]int) {
	if m == nil {
		m = make(map[string]int)
	}
	sb.toolCounts = m
}

// Restore restores all persistent status bar data from a snapshot.
func (sb *StatusBar) Restore(apiCalls int, toolCounts map[string]int, cacheRead, cacheCreation int) {
	sb.apiCalls = apiCalls
	if toolCounts == nil {
		toolCounts = make(map[string]int)
	}
	sb.toolCounts = toolCounts
	sb.cacheReadTokens = cacheRead
	sb.cacheCreationTokens = cacheCreation
}

// Render returns the status bar as one or two lines with ANSI-colored elements.
// Line 1: mode | model | ctx | tokens | scroll
// Line 2: api calls + tool counts (compact, only when tool info exists)
func (sb *StatusBar) Render(width int) string {
	var line1 []string

	// mode
	line1 = append(line1, sb.styles.Status.Model.Render(statusModeLabel(sb.mode)))

	// model name
	if sb.modelName != "" {
		line1 = append(line1, sb.styles.Status.Model.Render(sb.modelName))
	}

	// context
	if sb.contextWindow > 0 {
		remaining := sb.contextWindow - sb.contextUsed
		if remaining < 0 {
			remaining = 0
		}
		pct := remaining * 100 / sb.contextWindow
		line1 = append(line1, sb.styles.Status.Context.Render(fmt.Sprintf("ctx:%s/%s %d%%", formatTokenK(remaining), formatTokenK(sb.contextWindow), pct)))
	}

	// tokens + cache
	tokenPart := fmt.Sprintf("in/out/cache:%s/%s/%s", formatTokenK(sb.inputTokens), formatTokenK(sb.outputTokens), formatTokenK(sb.cacheReadTokens))
	if sb.inputTokens > 0 && (sb.cacheReadTokens > 0 || sb.cacheCreationTokens > 0) {
		hitRate := sb.cacheReadTokens * 100 / sb.inputTokens
		tokenPart += fmt.Sprintf(" %d%%", hitRate)
	}
	line1 = append(line1, sb.styles.Status.Tokens.Render(tokenPart))

	// scroll
	if sb.scrollPct > 0 {
		line1 = append(line1, sb.styles.Status.Scroll.Render(fmt.Sprintf("scroll:%d%%", sb.scrollPct)))
	}

	sep := sb.styles.Status.Separator.Render(" | ")
	l1 := strings.Join(line1, sep)

	// --- Line 2 (compact tool info) ---
	var line2Parts []string
	if sb.apiCalls > 0 {
		line2Parts = append(line2Parts, sb.styles.Status.Calls.Render(fmt.Sprintf("api:%d", sb.apiCalls)))
	}
	if len(sb.toolCounts) > 0 {
		names := make([]string, 0, len(sb.toolCounts))
		for n := range sb.toolCounts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			line2Parts = append(line2Parts, sb.styles.Status.Tool.Render(fmt.Sprintf("%s×%d", shortToolName(n), sb.toolCounts[n])))
		}
	}

	var lines []string
	if width > 0 {
		l1 = ansi.Truncate(l1, width, "")
	}
	lines = append(lines, l1)

	if len(line2Parts) > 0 {
		l2 := strings.Join(line2Parts, " ")
		if width > 0 {
			l2 = ansi.Truncate(l2, width, "")
		}
		lines = append(lines, l2)
	}

	return strings.Join(lines, "\n")
}

// Height returns the number of lines the status bar occupies (1 or 2).
func (sb *StatusBar) Height() int {
	if sb.apiCalls > 0 || len(sb.toolCounts) > 0 {
		return 2
	}
	return 1
}

// shortToolName returns a compact display name for the status bar.
var toolShortNames = map[string]string{
	"EnterPlanMode":  "Plan",
	"ExitPlanMode":   "Unplan",
	"AskUserQuestion": "Ask",
	"WebFetch":       "Web",
	"Compact":        "Cmpct",
}

func shortToolName(name string) string {
	if s, ok := toolShortNames[name]; ok {
		return s
	}
	// MCP tools: "mcp_serverName_toolName" → "serverName/toolName"
	if after, ok := strings.CutPrefix(name, "mcp_"); ok {
		if idx := strings.Index(after, "_"); idx >= 0 {
			return after[:idx] + "/" + after[idx+1:]
		}
		return after
	}
	return name
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
