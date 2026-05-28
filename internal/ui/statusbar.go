package ui

import (
	"fmt"
	"sort"
	"strings"
)

// StatusBar holds all data displayed in the bottom status line.
// It renders as plain text with "|" separators.
type StatusBar struct {
	// data
	modelName     string
	status        string
	busy          bool
	spinnerActive bool // true when status ends with "ing" — spinner animation
	statusFrame   int
	apiCalls      int
	toolCounts    map[string]int
	inputTokens      int
	outputTokens     int
	contextUsed      int
	contextWindow    int
	scrollPct        int
	cacheReadTokens  int
	cacheCreationTokens int
}

var statusSpinnerFrames = []rune{'-', '\\', '|', '/'}

// NewStatusBar creates a new StatusBar.
func NewStatusBar() *StatusBar {
	return &StatusBar{
		toolCounts: make(map[string]int),
	}
}

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

// Render returns the status bar as a single line of plain text with "|" separators.
func (sb *StatusBar) Render(width int) string {
	var parts []string

	// status (with spinner if active)
	if sb.status != "" {
		if sb.spinnerActive {
			frame := string(statusSpinnerFrames[sb.statusFrame%len(statusSpinnerFrames)])
			parts = append(parts, frame+" "+sb.status)
		} else {
			parts = append(parts, sb.status)
		}
	}

	// model name
	if sb.modelName != "" {
		parts = append(parts, sb.modelName)
	}

	// context
	if sb.contextWindow > 0 {
		remaining := sb.contextWindow - sb.contextUsed
		if remaining < 0 {
			remaining = 0
		}
		pct := remaining * 100 / sb.contextWindow
		parts = append(parts, fmt.Sprintf("ctx:%s/%s %d%%", formatTokenK(remaining), formatTokenK(sb.contextWindow), pct))
	}

	// tokens
	parts = append(parts, fmt.Sprintf("in/out:%s/%s", formatTokenK(sb.inputTokens), formatTokenK(sb.outputTokens)))

	// cache hit rate
	cacheTotal := sb.cacheReadTokens + sb.cacheCreationTokens
	if cacheTotal > 0 {
		hitRate := sb.cacheReadTokens * 100 / cacheTotal
		parts = append(parts, fmt.Sprintf("cache:%d%%", hitRate))
	}

	// api calls
	parts = append(parts, fmt.Sprintf("calls:%d", sb.apiCalls))

	// tool counts (sorted by name)
	if len(sb.toolCounts) > 0 {
		names := make([]string, 0, len(sb.toolCounts))
		for n := range sb.toolCounts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%s:%d", n, sb.toolCounts[n]))
		}
	}

	// scroll
	if sb.scrollPct > 0 {
		parts = append(parts, fmt.Sprintf("scroll:%d%%", sb.scrollPct))
	}

	line := strings.Join(parts, " | ")
	if width > 0 && len(line) > width {
		line = line[:width]
	}
	return line
}

// Height always returns 1 (single line).
func (sb *StatusBar) Height() int { return 1 }

func formatTokenK(n int) string {
	if n <= 0 {
		return "0K"
	}
	k := (n + 999) / 1000
	return fmt.Sprintf("%dK", k)
}
