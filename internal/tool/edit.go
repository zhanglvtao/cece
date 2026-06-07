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

type editTool struct{}

func NewEdit() Tool { return editTool{} }

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

func (editTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
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

// findActualString tries to find oldString in fileContent using a cascade of
// normalization strategies. It returns the byte index in the original fileContent
// and the actual substring from the file (preserving original whitespace/quotes).
//
// Cascade: exact → CRLF normalization → tab/space normalization →
// trailing whitespace normalization → quote normalization.
// Each step also tries trailing \n tolerance (add/strip one \n) if the base match fails.
func findActualString(fileContent, oldString string) (int, string) {
	// 1. Exact match
	if idx := strings.Index(fileContent, oldString); idx >= 0 {
		return idx, oldString
	}
	// 1b. Trailing \n tolerance on exact (for when old_string is missing or has extra \n)
	if idx, actual := findWithTrailingNewlineTolerance(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	// 2. CRLF → LF normalization
	normFile := strings.ReplaceAll(fileContent, "\r\n", "\n")
	normOld := strings.ReplaceAll(oldString, "\r\n", "\n")
	if idx := strings.Index(normFile, normOld); idx >= 0 {
		origStart := crlfNormToOrig(fileContent, idx)
		origEnd := crlfNormToOrig(fileContent, idx+len(normOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}
	// 2b. CRLF + trailing \n
	if idx, actual := findCRLFWithNewlineTolerance(fileContent, normFile, normOld); idx >= 0 {
		return idx, actual
	}

	// 3. Tab → 4 spaces normalization
	wsFile := expandTabs(fileContent)
	wsOld := expandTabs(oldString)
	if idx := strings.Index(wsFile, wsOld); idx >= 0 {
		origStart, origEnd := mapTabNormRangeBack(fileContent, wsFile, idx, len(wsOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}

	// 4. Trailing whitespace normalization (strip trailing spaces/tabs per line)
	twsFile := stripTrailingWS(normFile)
	twsOld := stripTrailingWS(normOld)
	if twsFile != normFile || twsOld != normOld { // skip if no-op
		if idx := strings.Index(twsFile, twsOld); idx >= 0 {
			origStart, origEnd := mapTrailingWSRangeBack(fileContent, twsFile, idx, len(twsOld))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}

	// 5. Quote normalization (curly → straight)
	qFile := normalizeQuotes(fileContent)
	qOld := normalizeQuotes(oldString)
	if idx := strings.Index(qFile, qOld); idx >= 0 {
		// Quote normalization changes byte length (curly=3 bytes, straight=1 byte).
		// We need to map the range back using a position mapper.
		origStart, origEnd := mapQuoteNormRangeBack(fileContent, qFile, idx, idx+len(qOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}

	return -1, ""
}

// findWithTrailingNewlineTolerance tries matching with an extra or missing trailing \n.
func findWithTrailingNewlineTolerance(fileContent, oldString string) (int, string) {
	// Try oldString + "\n" — LLM omitted trailing newline
	if !strings.HasSuffix(oldString, "\n") {
		extended := oldString + "\n"
		if idx := strings.Index(fileContent, extended); idx >= 0 {
			return idx, extended
		}
	}
	// Try oldString without trailing "\n" — LLM added extra newline
	if strings.HasSuffix(oldString, "\n") {
		trimmed := strings.TrimSuffix(oldString, "\n")
		if idx := strings.Index(fileContent, trimmed); idx >= 0 {
			return idx, trimmed
		}
	}
	return -1, ""
}

// findWithTrailingNewlineToleranceLast is the LastIndex variant.
func findWithTrailingNewlineToleranceLast(fileContent, oldString string) (int, string) {
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

// findCRLFWithNewlineTolerance does trailing \n tolerance under CRLF normalization.
func findCRLFWithNewlineTolerance(fileContent, normFile, normOld string) (int, string) {
	// Try normOld + "\n"
	if !strings.HasSuffix(normOld, "\n") {
		extended := normOld + "\n"
		if idx := strings.Index(normFile, extended); idx >= 0 {
			origStart := crlfNormToOrig(fileContent, idx)
			origEnd := crlfNormToOrig(fileContent, idx+len(extended))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}
	// Try normOld without trailing "\n"
	if strings.HasSuffix(normOld, "\n") {
		trimmed := strings.TrimSuffix(normOld, "\n")
		if idx := strings.Index(normFile, trimmed); idx >= 0 {
			origStart := crlfNormToOrig(fileContent, idx)
			origEnd := crlfNormToOrig(fileContent, idx+len(trimmed))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}
	return -1, ""
}

// findActualStringLast finds the last occurrence using the same cascade.
func findActualStringLast(fileContent, oldString string) (int, string) {
	// 1. Exact
	if idx := strings.LastIndex(fileContent, oldString); idx >= 0 {
		return idx, oldString
	}
	// 1b. Trailing \n tolerance
	if idx, actual := findWithTrailingNewlineToleranceLast(fileContent, oldString); idx >= 0 {
		return idx, actual
	}

	// 2. CRLF
	normFile := strings.ReplaceAll(fileContent, "\r\n", "\n")
	normOld := strings.ReplaceAll(oldString, "\r\n", "\n")
	if idx := strings.LastIndex(normFile, normOld); idx >= 0 {
		origStart := crlfNormToOrig(fileContent, idx)
		origEnd := crlfNormToOrig(fileContent, idx+len(normOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}
	// 2b. CRLF + trailing \n
	if idx, actual := findCRLFWithNewlineToleranceLast(fileContent, normFile, normOld); idx >= 0 {
		return idx, actual
	}

	// 3. Tab/space
	wsFile := expandTabs(fileContent)
	wsOld := expandTabs(oldString)
	if idx := strings.LastIndex(wsFile, wsOld); idx >= 0 {
		origStart, origEnd := mapTabNormRangeBack(fileContent, wsFile, idx, len(wsOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}

	// 4. Trailing whitespace normalization
	twsFile := stripTrailingWS(normFile)
	twsOld := stripTrailingWS(normOld)
	if twsFile != normFile || twsOld != normOld {
		if idx := strings.LastIndex(twsFile, twsOld); idx >= 0 {
			origStart, origEnd := mapTrailingWSRangeBack(fileContent, twsFile, idx, len(twsOld))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}

	// 5. Quote
	qFile := normalizeQuotes(fileContent)
	qOld := normalizeQuotes(oldString)
	if idx := strings.LastIndex(qFile, qOld); idx >= 0 {
		origStart, origEnd := mapQuoteNormRangeBack(fileContent, qFile, idx, idx+len(qOld))
		if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
			return origStart, fileContent[origStart:origEnd]
		}
	}

	return -1, ""
}

// findCRLFWithNewlineToleranceLast does trailing \n tolerance under CRLF normalization (last occurrence).
func findCRLFWithNewlineToleranceLast(fileContent, normFile, normOld string) (int, string) {
	if !strings.HasSuffix(normOld, "\n") {
		extended := normOld + "\n"
		if idx := strings.LastIndex(normFile, extended); idx >= 0 {
			origStart := crlfNormToOrig(fileContent, idx)
			origEnd := crlfNormToOrig(fileContent, idx+len(extended))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}
	if strings.HasSuffix(normOld, "\n") {
		trimmed := strings.TrimSuffix(normOld, "\n")
		if idx := strings.LastIndex(normFile, trimmed); idx >= 0 {
			origStart := crlfNormToOrig(fileContent, idx)
			origEnd := crlfNormToOrig(fileContent, idx+len(trimmed))
			if origStart >= 0 && origEnd >= 0 && origEnd <= len(fileContent) {
				return origStart, fileContent[origStart:origEnd]
			}
		}
	}
	return -1, ""
}

// findActualStringAll finds a matching string for replace_all mode.
func findActualStringAll(fileContent, oldString string) string {
	_, actual := findActualString(fileContent, oldString)
	return actual
}

// ── Normalization helpers ───────────────────────────────────────────────────

// expandTabs replaces each tab character with 4 spaces.
func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}

// normalizeQuotes replaces curly quotes with straight quotes.
func normalizeQuotes(s string) string {
	r := strings.NewReplacer(
		"\u2018", "'", // '
		"\u2019", "'", // '
		"\u201C", "\"", // "
		"\u201D", "\"", // "
	)
	return r.Replace(s)
}

// stripTrailingWS strips trailing whitespace from each line.
// Line endings (\n) are preserved. A final \n is preserved as an empty trailing line.
func stripTrailingWS(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// mapTrailingWSRangeBack maps a range [normStart, normStart+normLen) in the
// trailing-whitespace-stripped string back to [origStart, origEnd) in the original.
// Since stripping only shortens lines (never changes line boundaries), we walk
// both strings line-by-line to track the offset delta.
func mapTrailingWSRangeBack(orig, norm string, normStart, normLen int) (int, int) {
	// Fast path: identical → no mapping needed
	if orig == norm {
		return normStart, normStart + normLen
	}

	origPos := 0
	normPos := 0
	origStart := -1
	origEnd := -1

	for origPos < len(orig) && normPos <= normStart+normLen {
		if normPos == normStart {
			origStart = origPos
		}
		if normPos == normStart+normLen {
			origEnd = origPos
			break
		}

		// Walk to end of current line in both strings
		origLineEnd := strings.IndexByte(orig[origPos:], '\n')
		normLineEnd := strings.IndexByte(norm[normPos:], '\n')

		var origLineLen, normLineLen int
		if origLineEnd >= 0 {
			origLineLen = origLineEnd + 1 // include \n
		} else {
			origLineLen = len(orig) - origPos
		}
		if normLineEnd >= 0 {
			normLineLen = normLineEnd + 1
		} else {
			normLineLen = len(norm) - normPos
		}

		nextNormPos := normPos + normLineLen
		nextOrigPos := origPos + origLineLen

		// normStart falls within this line in norm — map to same line start in orig
		if normPos < normStart && nextNormPos > normStart && origStart == -1 {
			// Offset within the norm line
			withinNorm := normStart - normPos
			// The corresponding orig line (before stripping)
			origLine := orig[origPos:]
			if origLineEnd >= 0 {
				origLine = origLine[:origLineEnd]
			}
			normLine := norm[normPos:]
			if normLineEnd >= 0 {
				normLine = normLine[:normLineEnd]
			}
			// Walk character by character within the line
			oi, ni := 0, 0
			for oi < len(origLine) && ni < withinNorm {
				oi++
				ni++
			}
			origStart = origPos + oi
		}
		if normPos < normStart+normLen && nextNormPos > normStart+normLen && origEnd == -1 {
			withinNorm := normStart + normLen - normPos
			origLine := orig[origPos:]
			if origLineEnd >= 0 {
				origLine = origLine[:origLineEnd]
			}
			oi, ni := 0, 0
			for oi < len(origLine) && ni < withinNorm {
				oi++
				ni++
			}
			origEnd = origPos + oi
			break
		}

		normPos = nextNormPos
		origPos = nextOrigPos
	}

	if origStart == -1 {
		origStart = 0
	}
	if origEnd == -1 {
		origEnd = len(orig)
	}
	return origStart, origEnd
}

// crlfNormToOrig maps a byte offset in the CRLF-normalized string back
// to the byte offset in the original string.
// Each \r\n in orig becomes \n in norm, so norm is shorter.
// We walk orig, tracking how many \r\n we've skipped, to find the
// original byte position corresponding to normOffset.
func crlfNormToOrig(orig string, normOffset int) int {
	oi := 0  // byte offset in orig
	ni := 0  // byte offset in norm
	for oi < len(orig) && ni < normOffset {
		if oi+1 < len(orig) && orig[oi] == '\r' && orig[oi+1] == '\n' {
			// \r\n in orig → \n in norm: orig advances 2 bytes, norm advances 1
			oi += 2
			ni += 1
		} else {
			oi++
			ni++
		}
	}
	return oi
}

// mapTabNormRangeBack maps a range [normStart, normStart+normLen) in the
// tab-expanded string back to [origStart, origEnd) in the original string.
// Tab expands to 4 spaces, so we walk both strings to build the mapping.
func mapTabNormRangeBack(orig, norm string, normStart, normLen int) (int, int) {
	origPos := 0
	normPos := 0
	origStart := -1
	origEnd := -1

	for origPos < len(orig) && normPos <= normStart+normLen {
		if normPos == normStart {
			origStart = origPos
		}
		if normPos == normStart+normLen {
			origEnd = origPos
			break
		}

		if orig[origPos] == '\t' {
			nextNormPos := normPos + 4
			// normStart falls within an expanded tab — snap to tab position
			if normPos < normStart && nextNormPos > normStart && origStart == -1 {
				origStart = origPos
			}
			if normPos < normStart+normLen && nextNormPos > normStart+normLen && origEnd == -1 {
				origEnd = origPos + 1
				break
			}
			normPos = nextNormPos
			origPos++
		} else {
			normPos++
			origPos++
		}
	}

	if origStart == -1 {
		origStart = 0
	}
	if origEnd == -1 {
		origEnd = len(orig)
	}
	return origStart, origEnd
}

// mapQuoteNormRangeBack maps a range [normStart, normEnd) in the
// quote-normalized string back to [origStart, origEnd) in the original string.
// Curly quotes are 3 bytes in UTF-8, straight quotes are 1 byte.
// We walk both strings to build the mapping.
func mapQuoteNormRangeBack(orig, norm string, normStart, normEnd int) (int, int) {
	oi := 0 // byte offset in orig
	ni := 0 // byte offset in norm
	origStart := -1
	origEnd := -1

	for oi < len(orig) && ni < normEnd {
		if ni == normStart {
			origStart = oi
		}

		// Check if current position in orig is a curly quote (3 bytes → 1 byte in norm)
		if oi+2 < len(orig) {
			b3 := orig[oi : oi+3]
			if b3 == "\u2018" || b3 == "\u2019" || b3 == "\u201C" || b3 == "\u201D" {
				if ni+1 > normStart && origStart == -1 && ni < normStart {
					origStart = oi
				}
				if ni+1 > normEnd && origEnd == -1 && ni < normEnd {
					origEnd = oi
					break
				}
				oi += 3
				ni += 1
				continue
			}
		}

		if ni+1 > normStart && origStart == -1 && ni < normStart {
			origStart = oi
		}
		if ni+1 > normEnd && origEnd == -1 && ni < normEnd {
			origEnd = oi
			break
		}
		oi++
		ni++
	}

	if ni >= normEnd && origEnd == -1 {
		origEnd = oi
	}
	if origStart == -1 {
		origStart = 0
	}
	if origEnd == -1 {
		origEnd = len(orig)
	}
	return origStart, origEnd
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
