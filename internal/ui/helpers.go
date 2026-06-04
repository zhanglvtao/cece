package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"cece/internal/ui/theme"
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
// for display in the block title, e.g. "Edit path: /foo old_string: \"bar\"".
func formatToolTitleKVs(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return name
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return name
	}
	m, ok := v.(map[string]any)
	if !ok {
		return name
	}
	// For Edit/Write, only show path — old_string/new_string are too long.
	if name == "Edit" || name == "Write" {
		if p, ok := m["path"].(string); ok {
			return name + " " + p
		}
	}
	var parts []string
	for key, val := range m {
		parts = append(parts, key+": "+formatToolTitleValue(val))
	}
	return name + " " + strings.Join(parts, " ")
}

func formatToolTitleValue(val any) string {
	switch v := val.(type) {
	case string:
		// Truncate long strings
		s := v
		if len(s) > 100 {
			s = s[:50] + "..."
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
		if len(s) > 100 {
			s = s[:50] + "..."
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
