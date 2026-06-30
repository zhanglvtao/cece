package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhanglvtao/cece/internal/lint"
)

type editParams struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type editTool struct {
	tracker *ReadTracker
}

func NewEdit(tracker *ReadTracker) Tool { return editTool{tracker: tracker} }

func (editTool) Effect() Effect { return EffectWrite }

func (editTool) Info() Definition {
	return Definition{
		Name:        "Edit",
		Description: "Make precise string replacements in files. Returns a unified diff of changes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to edit",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "The text to find in the file. Must be an exact match and unique unless replace_all is true. If empty, creates a new file.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "The text to replace old_string with. If empty, deletes old_string.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace all occurrences of old_string (default: false, requires unique match)",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	}
}

func (t editTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p editParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.Path == "" {
		return Result{Content: "missing path", IsError: true}
	}
	if p.OldString == "" && p.NewString == "" {
		return Result{Content: "old_string and new_string are both empty", IsError: true}
	}

	// Create mode: old_string is empty → create new file
	if p.OldString == "" {
		if emitter != nil {
			emitter.Emit(fmt.Sprintf("Creating %s...", p.Path))
		}
		return editCreate(ctx, p.Path, p.NewString)
	}

	// Read existing file
	if !t.tracker.WasRead(p.Path) {
		return Result{Content: fmt.Sprintf("You must Read %s before editing it.", p.Path), IsError: true}
	}
	oldContent, err := os.ReadFile(p.Path)
	if err != nil {
		return Result{Content: fmt.Sprintf("read: %v", err), IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Editing %s...", p.Path))
	}

	s := string(oldContent)

	if p.ReplaceAll {
		actualOld := findActualStringAll(s, p.OldString)
		if actualOld == "" {
			return Result{Content: fmt.Sprintf("old_string not found in file.\n%s", editFileContext(s, p.OldString)), IsError: true}
		}
		newContent := strings.ReplaceAll(s, actualOld, p.NewString)
		diff := UnifiedDiff(p.Path, p.Path, s, newContent)
		if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
		}
		return lintAppend(ctx, p.Path, Result{Content: diff})
	}

	// Single replacement: must be unique
	idx, actualOld := findActualString(s, p.OldString)
	if idx < 0 {
		return Result{Content: fmt.Sprintf("old_string not found in file.\n%s", editFileContext(s, p.OldString)), IsError: true}
	}
	lastIdx, _ := findActualStringLast(s, p.OldString)
	if idx != lastIdx {
		return Result{Content: "old_string appears multiple times — use replace_all or provide more context to make it unique", IsError: true}
	}

	newContent := s[:idx] + p.NewString + s[idx+len(actualOld):]
	diff := UnifiedDiff(p.Path, p.Path, s, newContent)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
	}
	return lintAppend(ctx, p.Path, Result{Content: diff})
}

// ── Fuzzy matching cascade ──────────────────────────────────────────────────
//
// findActualString tries to find oldString in fileContent by generating
// candidate variants of oldString (zero-copy on fileContent) and searching
// with strings.Index. The cascade:
//
//	exact match → trailing \n tolerance → CRLF variants →
//	tab→spaces variant → curly→straight quotes variant →
//	trailing whitespace tolerance (line-by-line)
//
// Returns the byte index in fileContent and the actual matched substring.

func findActualString(fileContent, oldString string) (int, string) {
	// 1. Exact match
	if idx := strings.Index(fileContent, oldString); idx >= 0 {
		return idx, oldString
	}

	// 2. Trailing \n tolerance on exact
	if idx, actual := tryNewlineTolerance(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	// 3. Old-string variants (CRLF, tab/space, quotes)
	candidates := generateCandidates(oldString)
	for _, c := range candidates {
		if c == oldString {
			continue // already tried
		}
		if idx := strings.Index(fileContent, c); idx >= 0 {
			return idx, c
		}
		if idx, actual := tryNewlineTolerance(fileContent, c); idx >= 0 {
			return idx, actual
		}
	}

	// 4. Trailing whitespace tolerance (line-by-line, last resort)
	if idx, actual := findTrailingWSTolerant(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	return -1, ""
}

// generateCandidates produces old-string variants that account for common
// LLM rendering/encoding differences. Generates both directions for each
// transformation so we can match regardless of which side has which encoding.
func generateCandidates(s string) []string {
	var out []string

	// CRLF ↔ LF
	if strings.Contains(s, "\r\n") {
		out = append(out, strings.ReplaceAll(s, "\r\n", "\n"))
	} else if strings.Contains(s, "\n") {
		out = append(out, strings.ReplaceAll(s, "\n", "\r\n"))
	}

	// Tab ↔ 4 spaces
	if strings.Contains(s, "\t") {
		out = append(out, strings.ReplaceAll(s, "\t", "    "))
	}
	if strings.Contains(s, "    ") {
		// Only generate spaces→tab for leading/trailing whitespace per line
		out = append(out, spacesToTabs(s))
	}

	// Curly ↔ straight quotes
	if containsCurlyQuote(s) {
		out = append(out, normalizeQuotes(s))
	}
	if containsStraightQuote(s) {
		out = append(out, straightToCurlyQuotes(s))
	}

	return out
}

// spacesToTabs converts leading runs of 4 spaces to tabs in each line.
func spacesToTabs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		j := 0
		var b strings.Builder
		for j < len(line) {
			if j+4 <= len(line) && line[j:j+4] == "    " {
				b.WriteByte('\t')
				j += 4
			} else {
				break
			}
		}
		b.WriteString(line[j:])
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// containsStraightQuote reports whether s contains ASCII straight quotes.
func containsStraightQuote(s string) bool {
	return strings.ContainsAny(s, `"'`)
}

// straightToCurlyQuotes replaces straight quotes with curly equivalents.
func straightToCurlyQuotes(s string) string {
	var b strings.Builder
	prev := ' '
	for _, r := range s {
		switch r {
		case '"':
			if prev == ' ' || prev == '\n' || prev == '\t' || prev == '(' {
				b.WriteString("\u201C") // "
			} else {
				b.WriteString("\u201D") // "
			}
		case '\'':
			if prev == ' ' || prev == '\n' || prev == '\t' || prev == '(' {
				b.WriteString("\u2018") // '
			} else {
				b.WriteString("\u2019") // '
			}
		default:
			b.WriteRune(r)
		}
		prev = r
	}
	return b.String()
}

// tryNewlineTolerance tries matching with an extra or missing trailing \n.
func tryNewlineTolerance(fileContent, oldString string) (int, string) {
	if !strings.HasSuffix(oldString, "\n") {
		extended := oldString + "\n"
		if idx := strings.Index(fileContent, extended); idx >= 0 {
			return idx, extended
		}
	}
	if strings.HasSuffix(oldString, "\n") {
		trimmed := strings.TrimSuffix(oldString, "\n")
		if idx := strings.Index(fileContent, trimmed); idx >= 0 {
			return idx, trimmed
		}
	}
	return -1, ""
}

// findTrailingWSTolerant searches for oldString in fileContent line-by-line,
// ignoring trailing whitespace on each line. Returns the byte range of the
// actual (untrimmed) match in fileContent.
func findTrailingWSTolerant(fileContent, oldString string) (int, string) {
	oldLines := strings.Split(oldString, "\n")
	// Preserve trailing empty line indicator
	oldEndsWithNewline := len(oldLines) > 0 && oldLines[len(oldLines)-1] == ""
	if oldEndsWithNewline {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(oldLines) == 0 {
		return -1, ""
	}

	fileLines := strings.Split(fileContent, "\n")
	for i := 0; i <= len(fileLines)-len(oldLines); i++ {
		match := true
		hasDiff := false
		for j, ol := range oldLines {
			trimmed := strings.TrimRight(ol, " \t")
			fileTrimmed := strings.TrimRight(fileLines[i+j], " \t")
			if trimmed != fileTrimmed {
				match = false
				break
			}
			if ol != fileLines[i+j] {
				hasDiff = true
			}
		}
		if match && hasDiff {
			start := lineStartByte(fileContent, i)
			// End: include trailing ws on each matched line + \n separators
			// If old_string ends with \n, include the \n after the last line
			end := lineStartByte(fileContent, i+len(oldLines))
			if oldEndsWithNewline {
				// Include the trailing whitespace on the last line + \n
				// lineStartByte(i+len(oldLines)) already points past the \n
			} else {
				// Don't include the \n after the last matched line
				// Back up to end of content (before \n)
				if end > 0 && end <= len(fileContent) && fileContent[end-1] == '\n' {
					// end points to start of next line, so the \n is at end-1
					// We want to exclude the \n but include trailing ws on the last line
					// The last matched line's content is from lineStartByte(i+len(oldLines)-1) to end
					// We need just the content part (without \n)
					// Actually: end = lineStartByte of line after last matched
					// The last line content before \n is fileLines[i+len(oldLines)-1]
					// So the range should include that content but not the trailing \n
					lastLineStart := lineStartByte(fileContent, i+len(oldLines)-1)
					end = lastLineStart + len(fileLines[i+len(oldLines)-1])
				}
			}
			if end > len(fileContent) {
				end = len(fileContent)
			}
			return start, fileContent[start:end]
		}
	}
	return -1, ""
}

// lineStartByte returns the byte offset of the start of line n (0-based).
func lineStartByte(s string, line int) int {
	if line <= 0 {
		return 0
	}
	idx := 0
	for i := 0; i < line && idx < len(s); i++ {
		nl := strings.IndexByte(s[idx:], '\n')
		if nl < 0 {
			return len(s)
		}
		idx += nl + 1
	}
	return idx
}

// containsCurlyQuote reports whether s contains any curly quote characters.
func containsCurlyQuote(s string) bool {
	return strings.ContainsAny(s, "\u2018\u2019\u201C\u201D")
}

// normalizeQuotes replaces curly quotes with straight quotes.
func normalizeQuotes(s string) string {
	r := strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201C", "\"",
		"\u201D", "\"",
	)
	return r.Replace(s)
}

// findActualStringLast finds the last occurrence using the same cascade.
func findActualStringLast(fileContent, oldString string) (int, string) {
	// 1. Exact
	if idx := strings.LastIndex(fileContent, oldString); idx >= 0 {
		return idx, oldString
	}

	// 2. Trailing \n tolerance
	if idx, actual := tryNewlineToleranceLast(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	// 3. Variants
	candidates := generateCandidates(oldString)
	for _, c := range candidates {
		if c == oldString {
			continue
		}
		if idx := strings.LastIndex(fileContent, c); idx >= 0 {
			return idx, c
		}
		if idx, actual := tryNewlineToleranceLast(fileContent, c); idx >= 0 {
			return idx, actual
		}
	}

	// 4. Trailing whitespace tolerance
	if idx, actual := findTrailingWSTolerantLast(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	return -1, ""
}

func tryNewlineToleranceLast(fileContent, oldString string) (int, string) {
	if !strings.HasSuffix(oldString, "\n") {
		extended := oldString + "\n"
		if idx := strings.LastIndex(fileContent, extended); idx >= 0 {
			return idx, extended
		}
	}
	if strings.HasSuffix(oldString, "\n") {
		trimmed := strings.TrimSuffix(oldString, "\n")
		if idx := strings.LastIndex(fileContent, trimmed); idx >= 0 {
			return idx, trimmed
		}
	}
	return -1, ""
}

func findTrailingWSTolerantLast(fileContent, oldString string) (int, string) {
	oldLines := strings.Split(oldString, "\n")
	oldEndsWithNewline := len(oldLines) > 0 && oldLines[len(oldLines)-1] == ""
	if oldEndsWithNewline {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(oldLines) == 0 {
		return -1, ""
	}

	fileLines := strings.Split(fileContent, "\n")
	for i := len(fileLines) - len(oldLines); i >= 0; i-- {
		match := true
		hasDiff := false
		for j, ol := range oldLines {
			trimmed := strings.TrimRight(ol, " \t")
			fileTrimmed := strings.TrimRight(fileLines[i+j], " \t")
			if trimmed != fileTrimmed {
				match = false
				break
			}
			if ol != fileLines[i+j] {
				hasDiff = true
			}
		}
		if match && hasDiff {
			start := lineStartByte(fileContent, i)
			end := lineStartByte(fileContent, i+len(oldLines))
			if !oldEndsWithNewline {
				if end > 0 && end <= len(fileContent) && fileContent[end-1] == '\n' {
					lastLineStart := lineStartByte(fileContent, i+len(oldLines)-1)
					end = lastLineStart + len(fileLines[i+len(oldLines)-1])
				}
			}
			if end > len(fileContent) {
				end = len(fileContent)
			}
			return start, fileContent[start:end]
		}
	}
	return -1, ""
}

// findActualStringAll finds a matching string for replace_all mode.
func findActualStringAll(fileContent, oldString string) string {
	_, actual := findActualString(fileContent, oldString)
	return actual
}

// ── Error context ───────────────────────────────────────────────────────────

// editFileContext returns a snippet of the file content around where
// old_string might be expected, to help the LLM self-correct.
func editFileContext(fileContent, oldString string) string {
	lines := strings.Split(fileContent, "\n")

	// Try to find a partial match (first line of oldString)
	firstLine := oldString
	if idx := strings.Index(oldString, "\n"); idx >= 0 {
		firstLine = oldString[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)

	matchLine := -1
	if firstLine != "" {
		// Try trimmed match
		for i, line := range lines {
			if strings.Contains(strings.TrimSpace(line), firstLine) {
				matchLine = i
				break
			}
		}
		// Fallback: first word
		if matchLine < 0 {
			words := strings.Fields(firstLine)
			if len(words) > 0 {
				for i, line := range lines {
					if strings.Contains(line, words[0]) {
						matchLine = i
						break
					}
				}
			}
		}
	}

	if matchLine < 0 {
		// No clue — show first 20 lines
		end := 20
		if end > len(lines) {
			end = len(lines)
		}
		return fmt.Sprintf("File content (first %d lines):\n%s", end, addLineNumbers(strings.Join(lines[:end], "\n")))
	}

	// Show ±5 lines around match
	start := matchLine - 5
	if start < 0 {
		start = 0
	}
	end := matchLine + 6
	if end > len(lines) {
		end = len(lines)
	}

	return fmt.Sprintf("File content near expected location (lines %d-%d):\n%s", start+1, end, addLineNumbers(strings.Join(lines[start:end], "\n")))
}

// addLineNumbers prefixes each line with its 1-based line number.
func addLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	// Remove trailing empty line from final \n
	trailing := ""
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		trailing = "\n"
		lines = lines[:len(lines)-1]
	}
	width := len(fmt.Sprintf("%d", len(lines)))
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%*d | %s\n", width, i+1, line)
	}
	return b.String() + trailing
}

// ── Lint append helper ────────────────────────────────────────────────────────

// lintAppend runs the lint command for the file if a Runner is available.
// Only appends output when lint finds issues; returns r unchanged on success.
func lintAppend(ctx context.Context, filePath string, r Result) Result {
	if runner := lint.FromContext(ctx); runner != nil {
		if out := runner.Run(ctx, filePath); out != "" {
			r.Content += "\n" + out
		}
	}
	return r
}

// ── Create file ─────────────────────────────────────────────────────────────

func editCreate(ctx context.Context, path, content string) Result {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return Result{Content: "file already exists — use old_string to edit it", IsError: true}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{Content: fmt.Sprintf("mkdir: %v", err), IsError: true}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
	}

	diff := UnifiedDiff(path, path, "", content)
	return lintAppend(ctx, path, Result{Content: diff})
}
