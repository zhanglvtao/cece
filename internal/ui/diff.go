package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// DiffStyles defines styles for rendering inline diff output.
type DiffStyles struct {
	DeleteLine  lipgloss.Style
	InsertLine  lipgloss.Style
	ContextLine lipgloss.Style
	Summary     lipgloss.Style
}

// hunkHeaderRe matches @@ -oldStart,oldCount +newStart,newCount @@
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// diffLine represents a parsed line from unified diff for inline rendering.
type diffLine struct {
	kind    string // "delete", "insert", "context"
	lineNum int    // display line number (old for delete, new for insert/context)
	content string // line content without prefix
}

// RenderDiff parses a unified diff string and renders it as an inline diff
// with line numbers, +/- markers, and a summary line.
func RenderDiff(diff string, styles DiffStyles, width int) string {
	if diff == "" {
		return ""
	}

	parsed := parseUnifiedDiff(diff)
	summary := buildDiffSummary(parsed)
	rendered := renderInlineDiffLines(parsed, styles, width)

	var b strings.Builder
	b.WriteString(styles.Summary.Render(summary))
	for _, line := range rendered {
		b.WriteByte('\n')
		b.WriteString(line)
	}
	return b.String()
}

// parseUnifiedDiff converts unified diff text into structured diffLines.
func parseUnifiedDiff(diff string) []diffLine {
	rawLines := strings.Split(diff, "\n")
	var result []diffLine

	oldLine, newLine := 0, 0

	for _, raw := range rawLines {
		if strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ") {
			continue
		}

		if m := hunkHeaderRe.FindStringSubmatch(raw); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[3])
			continue
		}

		if raw == "" {
			continue
		}

		switch {
		case strings.HasPrefix(raw, "-"):
			result = append(result, diffLine{
				kind:    "delete",
				lineNum: oldLine,
				content: strings.TrimPrefix(raw, "-"),
			})
			oldLine++
		case strings.HasPrefix(raw, "+"):
			result = append(result, diffLine{
				kind:    "insert",
				lineNum: newLine,
				content: strings.TrimPrefix(raw, "+"),
			})
			newLine++
		default:
			content := raw
			if strings.HasPrefix(raw, " ") {
				content = raw[1:]
			}
			result = append(result, diffLine{
				kind:    "context",
				lineNum: newLine,
				content: content,
			})
			oldLine++
			newLine++
		}
	}

	return result
}

// buildDiffSummary creates the "Added N lines, removed M lines" summary.
func buildDiffSummary(lines []diffLine) string {
	added, removed := 0, 0
	for _, l := range lines {
		switch l.kind {
		case "insert":
			added++
		case "delete":
			removed++
		}
	}
	return fmt.Sprintf("Added %d lines, removed %d lines", added, removed)
}

// renderInlineDiffLines renders each diffLine as a single colored row:
// line number + marker + content, all under the same background style.
func renderInlineDiffLines(lines []diffLine, styles DiffStyles, width int) []string {
	numWidth := 3
	maxNum := 0
	for _, l := range lines {
		if l.lineNum > maxNum {
			maxNum = l.lineNum
		}
	}
	nw := len(strconv.Itoa(maxNum))
	if nw > numWidth {
		numWidth = nw
	}

	var result []string
	for _, l := range lines {
		result = append(result, renderInlineDiffRow(l, styles, numWidth, width))
	}
	return result
}

func renderInlineDiffRow(l diffLine, styles DiffStyles, numWidth, width int) string {
	numStr := fmt.Sprintf("%*d", numWidth, l.lineNum)

	switch l.kind {
	case "delete":
		row := fmt.Sprintf("%s - %s", numStr, l.content)
		return styles.DeleteLine.Width(width).Render(row)

	case "insert":
		row := fmt.Sprintf("%s + %s", numStr, l.content)
		return styles.InsertLine.Width(width).Render(row)

	default: // context
		row := fmt.Sprintf("%s   %s", numStr, l.content)
		return styles.ContextLine.Width(width).Render(row)
	}
}
