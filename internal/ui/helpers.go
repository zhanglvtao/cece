package ui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/zhanglvtao/cece/internal/ui/theme"
)

const (
	toolPreviewBytes    = 2000
	toolPreviewMaxLines = 3
	diffPreviewMaxLines = 20
)

var (
	diffStyleDel   = lipgloss.NewStyle().Foreground(theme.Red)
	diffStyleAdd   = lipgloss.NewStyle().Foreground(theme.Green)
	diffStyleHunk  = lipgloss.NewStyle().Foreground(theme.Primary)
	diffStyleHeader = lipgloss.NewStyle().Foreground(theme.FgMuted)
)

// renderDiffText applies ANSI colors to unified diff output:
// red for deletions, green for insertions, cyan for hunk headers.
func renderDiffText(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			lines[i] = diffStyleHeader.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = diffStyleDel.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = diffStyleAdd.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = diffStyleHunk.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// isQuietTool returns true for tools whose output should not be displayed in the UI.
func isQuietTool(name string) bool {
	return name == "Read" || name == "Grep" || name == "Glob"
}

// isDiffTool returns true for tools whose output is unified diff format.
func isDiffTool(name string) bool {
	return name == "Edit" || name == "Write"
}

// isExecTool returns true for tools that execute commands (Bash).
func isExecTool(name string) bool {
	return name == "Bash"
}

// diffAwareMaxLines returns diffPreviewMaxLines if the content looks like
// unified diff output, otherwise toolPreviewMaxLines.
func diffAwareMaxLines(content string) int {
	if strings.Contains(content, "--- a/") || strings.Contains(content, "+++ b/") {
		return diffPreviewMaxLines
	}
	return toolPreviewMaxLines
}

func formatJSONPreview(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return summarizeText(string(raw), 1000, 15)
	}
	m, ok := v.(map[string]any)
	if !ok {
		compact, err := json.Marshal(v)
		if err != nil {
			return summarizeText(string(raw), 1000, 15)
		}
		return summarizeText(string(compact), 1000, 15)
	}
	var lines []string
	for key, val := range m {
		compact, err := json.Marshal(val)
		if err != nil {
			lines = append(lines, key+": "+fmt.Sprint(val))
		} else {
			lines = append(lines, key+": "+string(compact))
		}
	}
	return summarizeText(strings.Join(lines, "\n"), 1000, 15)
}

// formatToolTitleKVs formats a tool call's input as a single-line KV string
// for display in the block title. Returns (name, params) separately so the
// renderer can color them differently — name gets highlighted, params do not.
func formatToolTitleKVs(name string, raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return name, ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return name, ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return name, ""
	}
	// All tools show full KV params for maximum clarity in one line.
	// Sort keys for deterministic output; put important keys first.
	priorityKeys := []string{"path", "pattern", "command", "query", "url", "prompt"}
	keyOrder := make(map[string]int)
	for i, k := range priorityKeys {
		keyOrder[k] = i
	}
	sortedKeys := make([]string, 0, len(m))
	for k := range m {
		sortedKeys = append(sortedKeys, k)
	}
	slices.SortFunc(sortedKeys, func(a, b string) int {
		ai, aOk := keyOrder[a]
		bi, bOk := keyOrder[b]
		switch {
		case aOk && bOk:
			return cmp.Compare(ai, bi)
		case aOk:
			return -1
		case bOk:
			return 1
		default:
			return cmp.Compare(a, b)
		}
	})
	var parts []string
	for _, key := range sortedKeys {
		parts = append(parts, key+": "+formatToolTitleValue(m[key]))
	}
	return toolDisplayName(name), strings.Join(parts, " ")
}

func formatToolTitleValue(val any) string {
	switch v := val.(type) {
	case string:
		// Truncate long strings to keep the title line compact.
		s := v
		if len(s) > 60 {
			s = s[:30] + "..."
		}
		// Quote if contains spaces or special characters
		if strings.ContainsAny(s, " \t\n\"'{}[]") {
			s = strings.ReplaceAll(s, "\"", "\\\"")
			s = strings.ReplaceAll(s, "\n", "\\n")
			return `"` + s + `"`
		}
		return s
	case nil:
		return "null"
	default:
		compact, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		s := string(compact)
		if len(s) > 60 {
			s = s[:30] + "..."
		}
		return s
	}
}

// formatToolPreview formats a tool call's input for the transcript.
// For the Agent tool, it shows a compact summary: description + prompt excerpt.
// For all other tools, it falls through to formatJSONPreview.
func formatToolPreview(name string, raw json.RawMessage) string {
	if name != "Agent" || len(raw) == 0 {
		return formatJSONPreview(raw)
	}
	var p struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return formatJSONPreview(raw)
	}
	var b strings.Builder
	if p.Description != "" {
		b.WriteString(p.Description)
		b.WriteString("\n")
	}
	if p.Prompt != "" {
		promptPreview := p.Prompt
		if len(promptPreview) > 200 {
			promptPreview = promptPreview[:200] + "..."
		}
		// Show first few lines
		lines := strings.Split(promptPreview, "\n")
		maxLines := 5
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, "...")
		}
		b.WriteString(strings.Join(lines, "\n"))
	}
	return b.String()
}

func summarizeText(s string, maxBytes, maxLines int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	truncated := false
	if len(s) > maxBytes {
		s = s[:maxBytes]
		truncated = true
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		kept := make([]string, 0, maxLines+1)
		kept = append(kept, lines[:maxLines]...)
		kept = append(kept, fmt.Sprintf("... %d lines hidden ...", len(lines)-maxLines))
		lines = kept
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += "\n... truncated ..."
	}
	return out
}

// compactExecResult compresses multi-line exec output into a one-line summary:
// "→ ok: item1, item2, item3 ... (N more)"
func compactExecResult(text string, isError bool) string {
	prefix := "→ ok"
	if isError {
		prefix = "→ error"
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return prefix
	}
	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	// Build a single-line summary from first few lines.
	items := make([]string, 0, 3)
	maxItems := 3
	for i := 0; i < len(lines) && i < maxItems; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if len(line) > 60 {
			line = line[:57] + "..."
		}
		items = append(items, line)
	}

	summary := strings.Join(items, ", ")
	if len(items) == 0 {
		return prefix
	}
	if totalLines > maxItems {
		summary += fmt.Sprintf(" ... (%d more)", totalLines-len(items))
	}
	return prefix + ": " + summary
}

func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
