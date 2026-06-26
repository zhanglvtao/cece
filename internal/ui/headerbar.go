package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"
)

type toolDisplayMeta struct {
	displayName string
	group       int
	order       int
}

const (
	toolGroupFile = iota
	toolGroupWeb
	toolGroupAsk
	toolGroupContext
	toolGroupAgent
	toolGroupPlan
	toolGroupDefault
)

var toolDisplayMetaMap = map[string]toolDisplayMeta{
	"Read":            {displayName: "Read", group: toolGroupFile, order: 0},
	"Write":           {displayName: "Write", group: toolGroupFile, order: 1},
	"Edit":            {displayName: "Edit", group: toolGroupFile, order: 2},
	"Glob":            {displayName: "Glob", group: toolGroupFile, order: 3},
	"Grep":            {displayName: "Grep", group: toolGroupFile, order: 4},
	"Bash":            {displayName: "Bash", group: toolGroupFile, order: 5},
	"WebFetch":        {displayName: "WebFetch", group: toolGroupWeb, order: 0},
	"WebSearch":       {displayName: "WebSearch", group: toolGroupWeb, order: 1},
	"AskUserQuestion": {displayName: "Ask", group: toolGroupAsk, order: 0},
	"Compact":         {displayName: "Compact", group: toolGroupContext, order: 0},
	"Trim":            {displayName: "Trim", group: toolGroupContext, order: 1},
	"Prune":           {displayName: "Prune", group: toolGroupContext, order: 2},
	"Truncate":        {displayName: "Truncate", group: toolGroupContext, order: 3},
	"Agent":           {displayName: "Agent", group: toolGroupAgent, order: 0},
	"EnterPlanMode":   {displayName: "EnterPlan", group: toolGroupPlan, order: 0},
	"ExitPlanMode":    {displayName: "ExitPlan", group: toolGroupPlan, order: 1},
}

func toolDisplayName(name string) string {
	if meta, ok := toolDisplayMetaMap[name]; ok && meta.displayName != "" {
		return meta.displayName
	}
	return name
}

func toolMeta(name string) toolDisplayMeta {
	if meta, ok := toolDisplayMetaMap[name]; ok {
		return meta
	}
	return toolDisplayMeta{displayName: name, group: toolGroupDefault, order: 1 << 30}
}

// HeaderBar displays cumulative statistics at the top of the TUI.
// It tracks API calls, tool calls, turns (each with success/failure counts),
// and token usage (in/out/cache).
type HeaderBar struct {
	styles Styles

	apiOK   int
	apiFail int

	toolOK   map[string]int
	toolFail map[string]int

	turnOK   int
	turnFail int

	completionHooks int
	inputTokens     int
	outputTokens    int
	cacheReadTokens int
}

// NewHeaderBar creates a new HeaderBar.
func NewHeaderBar() *HeaderBar {
	return &HeaderBar{
		styles:   DefaultStyles(),
		toolOK:   make(map[string]int),
		toolFail: make(map[string]int),
	}
}

// IncrementAPI adds one to the API call counter (success or failure).
func (h *HeaderBar) IncrementAPI(ok bool) {
	if ok {
		h.apiOK++
	} else {
		h.apiFail++
	}
}

// IncrementTool adds one to the per-tool success/failure counter.
func (h *HeaderBar) IncrementTool(name string, ok bool) {
	if ok {
		h.toolOK[name]++
	} else {
		h.toolFail[name]++
	}
}

// IncrementTurn adds one to the turn counter (success or failure).
func (h *HeaderBar) IncrementTurn(ok bool) {
	if ok {
		h.turnOK++
	} else {
		h.turnFail++
	}
}

// UpdateTokens updates token statistics.
func (h *HeaderBar) UpdateTokens(input, output, cacheRead int) {
	h.inputTokens = input
	h.outputTokens = output
	h.cacheReadTokens = cacheRead
}

// IncrementCompletionHook adds one to the completion hook counter.
func (h *HeaderBar) IncrementCompletionHook() { h.completionHooks++ }

// Restore restores cumulative counters from a saved snapshot.
// On restore, all existing counts are treated as successes since
// the snapshot does not differentiate success/failure.
func (h *HeaderBar) Restore(apiCalls int, toolCounts map[string]int, cacheRead int, turnCount int, completionHookCalls int) {
	h.apiOK = apiCalls
	h.apiFail = 0

	h.toolOK = make(map[string]int)
	h.toolFail = make(map[string]int)
	for name, count := range toolCounts {
		h.toolOK[name] = count
	}

	h.turnOK = turnCount
	h.turnFail = 0
	h.completionHooks = completionHookCalls
	
	h.cacheReadTokens = cacheRead
}

// Height returns the number of lines (always 1).
func (h *HeaderBar) Height() int { return 1 }

// Render renders the header bar as a single line.
func (h *HeaderBar) Render(width int) string {
	var parts []string

	// API calls
	parts = append(parts, h.formatStatGroup("API", h.apiOK, h.apiFail))

	// Tool calls — per-tool breakdown with per-tool ✓/✗
	parts = append(parts, h.formatToolGroup())

	// Turns
	parts = append(parts, h.formatStatGroup("Turn", h.turnOK, h.turnFail))

	// Completion hooks
	parts = append(parts, h.styles.Status.Tokens.Render(fmt.Sprintf("Hook %d", h.completionHooks)))

	// Tokens
	tokenPart := fmt.Sprintf("in/out/cache:%s/%s/%s",
		formatTokenK(h.inputTokens),
		formatTokenK(h.outputTokens),
		formatTokenK(h.cacheReadTokens),
	)
	parts = append(parts, h.styles.Status.Tokens.Render(tokenPart))

	sep := h.styles.Status.Separator.Render(" │ ")
	line := strings.Join(parts, sep)
	if width > 0 {
		line = ansi.Truncate(line, width, "")
	}
	return line
}

// formatStatGroup renders a label with colored ✓/✗ counts.
func (h *HeaderBar) formatStatGroup(label string, ok, fail int) string {
	var b strings.Builder
	b.WriteString(label)
	b.WriteString(" ")
	b.WriteString(h.styles.Status.Ok.Render(fmt.Sprintf("✓%d", ok)))
	if fail > 0 {
		b.WriteString(h.styles.Status.Fail.Render(fmt.Sprintf("✗%d", fail)))
	}
	return b.String()
}

// formatToolGroup renders per-tool entries with colored tool name and ✓/✗.
func (h *HeaderBar) formatToolGroup() string {
	// collect all tool names
	names := make(map[string]bool)
	for name := range h.toolOK {
		names[name] = true
	}
	for name := range h.toolFail {
		names[name] = true
	}
	if len(names) == 0 {
		return "Tool ✓0"
	}

	type kv struct {
		name  string
		ok    int
		fail  int
		meta  toolDisplayMeta
		total int
	}
	sorted := make([]kv, 0, len(names))
	for name := range names {
		sorted = append(sorted, kv{name: name, ok: h.toolOK[name], fail: h.toolFail[name], meta: toolMeta(name), total: h.toolOK[name] + h.toolFail[name]})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].meta.group != sorted[j].meta.group {
			return sorted[i].meta.group < sorted[j].meta.group
		}
		if sorted[i].meta.order != sorted[j].meta.order {
			return sorted[i].meta.order < sorted[j].meta.order
		}
		if sorted[i].total != sorted[j].total {
			return sorted[i].total > sorted[j].total
		}
		return sorted[i].name < sorted[j].name
	})

	var b strings.Builder
	for _, t := range sorted {
		sty := h.toolStyle(t.name)
		name := sty.Render(t.meta.displayName)
		b.WriteString(fmt.Sprintf("%s ✓%d", name, t.ok))
		if t.fail > 0 {
			b.WriteString(h.styles.Status.Fail.Render(fmt.Sprintf("✗%d", t.fail)))
		}
		b.WriteString("  ")
	}
	return strings.TrimSpace(b.String())
}

// toolStyle returns the lipgloss style for a given tool name.
func (h *HeaderBar) toolStyle(name string) lipgloss.Style {
	switch name {
	case "Read", "Write", "Edit", "Glob", "Grep", "Bash":
		return h.styles.Status.ToolFile
	case "WebFetch", "WebSearch":
		return h.styles.Status.ToolWeb
	case "AskUserQuestion":
		return h.styles.Status.ToolAsk
	case "Compact", "Trim", "Prune", "Truncate":
		return h.styles.Status.ToolCtx
	case "Agent":
		return h.styles.Status.ToolAgent
	case "EnterPlanMode", "ExitPlanMode":
		return h.styles.Status.ToolPlan
	default:
		return h.styles.Status.Tool
	}
}