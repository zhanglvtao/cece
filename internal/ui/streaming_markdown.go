package ui

import (
	"strings"

	"charm.land/glamour/v2"
)

// streamingMarkdown caches a "stable prefix" glamour render so each
// streaming flush only re-renders the trailing portion of the document.
//
// The boundary between "stable" and "trailing" is detected by
// findSafeMarkdownBoundary: a position immediately after a blank line
// at which we can prove no markdown construct is open (fenced code
// block, list, table, block quote, setext header).
//
// Two renders concatenated are NOT generally equal to a single render
// of the whole document — glamour's wrap state is reset between calls.
// The boundary check is therefore deliberately conservative; whenever
// it has the slightest doubt the call falls back to a full render and
// the cache is left untouched.
type streamingMarkdown struct {
	width              int
	stablePrefix       string
	stablePrefixRender string
}

// Reset drops every cached field. After Reset the next Render call
// is guaranteed to be a full render.
func (s *streamingMarkdown) Reset() {
	s.width = 0
	s.stablePrefix = ""
	s.stablePrefixRender = ""
}

// Render returns the glamour render of content at the given width,
// reusing the cached stable-prefix render when it is safe to do so.
// On any uncertainty the call falls back to a full render via renderer
// and leaves the cache untouched (or drops it).
func (s *streamingMarkdown) Render(content string, width int, renderer *glamour.TermRenderer) string {
	full := func() string {
		out, err := renderer.Render(content)
		if err != nil {
			return content
		}
		return strings.TrimSuffix(out, "\n")
	}

	// Width change OR content not a prefix-extension: drop cache,
	// full render, optionally try to seed a fresh boundary.
	if width != s.width || !strings.HasPrefix(content, s.stablePrefix) {
		s.Reset()
		s.width = width
		out := full()
		s.tryAdvanceFromEmpty(content, width, renderer)
		return out
	}

	boundary := findSafeMarkdownBoundary(content)
	if boundary < 0 {
		// No safe boundary anywhere yet. Full render; do not
		// modify the cache (a future flush may find one).
		return full()
	}

	if boundary <= len(s.stablePrefix) {
		// Cached prefix already covers an at-least-as-late
		// boundary. Render the trailing partial fresh and glue.
		trail := content[len(s.stablePrefix):]
		return glueRenders(s.stablePrefixRender, s.renderTrailing(trail, renderer))
	}

	// boundary > len(stablePrefix): we have a NEW chunk of safe
	// content. Render the new chunk, append to stablePrefixRender,
	// promote the boundary, then render the remaining trail.
	newChunk := content[len(s.stablePrefix):boundary]
	newChunkRender := s.renderTrailing(newChunk, renderer)
	s.stablePrefixRender = glueRenders(s.stablePrefixRender, newChunkRender)
	s.stablePrefix = content[:boundary]

	trail := content[boundary:]
	if trail == "" {
		return s.stablePrefixRender
	}
	return glueRenders(s.stablePrefixRender, s.renderTrailing(trail, renderer))
}

// tryAdvanceFromEmpty seeds the cache from a fresh state. We've
// already paid the cost of a full render of content; if there is
// a safe boundary inside it, render the prefix once more and cache
// it so the next flush can avoid the full work.
func (s *streamingMarkdown) tryAdvanceFromEmpty(content string, width int, renderer *glamour.TermRenderer) {
	boundary := findSafeMarkdownBoundary(content)
	if boundary <= 0 {
		return
	}
	prefix := content[:boundary]
	out, err := renderer.Render(prefix)
	if err != nil {
		return
	}
	s.stablePrefix = prefix
	s.stablePrefixRender = trimGlamourMargins(out)
	s.width = width
}

// renderTrailing renders a trailing partial as a fresh glamour
// document and trims the surrounding whitespace so it can be
// concatenated to a cached prefix render.
func (s *streamingMarkdown) renderTrailing(text string, renderer *glamour.TermRenderer) string {
	if text == "" {
		return ""
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return trimGlamourMargins(out)
}

// glueRenders concatenates two glamour-rendered fragments with a
// single blank line separator.
func glueRenders(prefix, trail string) string {
	prefix = trimGlamourMargins(prefix)
	trail = trimGlamourMargins(trail)
	switch {
	case prefix == "" && trail == "":
		return ""
	case prefix == "":
		return trail
	case trail == "":
		return prefix
	default:
		return prefix + "\n\n" + trail
	}
}

// trimGlamourMargins strips leading and trailing whitespace
// (including newlines) from a glamour-rendered fragment.
func trimGlamourMargins(s string) string {
	return strings.Trim(s, " \t\n")
}

// ── Boundary Detection ───────────────────────────────────────────────

// findSafeMarkdownBoundary returns the byte offset of the END of
// the latest safe boundary in content, i.e. the offset such that
// content[:boundary] is a valid stable-prefix candidate.
// Returns -1 when no safe boundary exists.
func findSafeMarkdownBoundary(content string) int {
	if len(content) == 0 {
		return -1
	}

	for p := blankLineBefore(content, len(content)); p > 0; p = blankLineBefore(content, p-1) {
		if isSafeBoundaryAt(content, p) {
			return p
		}
	}
	return -1
}

// blankLineBefore returns the byte offset of the first character
// AFTER the latest blank-line separator that ends strictly before
// until. Returns -1 when no blank-line separator exists.
func blankLineBefore(content string, until int) int {
	if until <= 0 {
		return -1
	}
	end := until
	for end > 0 {
		nl := strings.LastIndexByte(content[:end], '\n')
		if nl < 0 {
			return -1
		}
		prev := strings.LastIndexByte(content[:nl], '\n')
		for prev >= 0 {
			gap := content[prev+1 : nl]
			if isBlankOrSpaces(gap) {
				return nl + 1
			}
			break
		}
		end = nl
	}
	return -1
}

func isBlankOrSpaces(s string) bool {
	for i := range len(s) {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// isSafeBoundaryAt reports whether content[:p] is a safe stable prefix.
func isSafeBoundaryAt(content string, p int) bool {
	prefix := content[:p]

	if countFenceLines(prefix)%2 != 0 {
		return false
	}

	if prefixHasOpenHazard(prefix) {
		return false
	}

	lastLine := lastNonBlankLine(prefix)
	if lastLine != "" && lineOpensConstruct(lastLine) {
		return false
	}

	if rest := content[p:]; rest != "" {
		first := firstNonBlankLine(rest)
		if isSetextUnderlineCandidate(first) {
			return false
		}
	}

	return true
}

// prefixHasOpenHazard reports whether prefix contains any construct
// that cannot be safely cut at a blank-line boundary.
func prefixHasOpenHazard(prefix string) bool {
	inFence := false
	for line := range splitLines(prefix) {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if isListItemMarker(trimmed) {
			return true
		}
		if isHTMLBlockOpener(line) {
			return true
		}
		if isLinkRefDefinition(line) {
			return true
		}
	}
	return false
}

func countFenceLines(s string) int {
	n := 0
	for line := range splitLines(s) {
		if isFenceLine(line) {
			n++
		}
	}
	return n
}

func isFenceLine(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return false
	}
	run := 0
	for i < len(line) && line[i] == c {
		i++
		run++
	}
	return run >= 3
}

func lastNonBlankLine(s string) string {
	last := ""
	for line := range splitLines(s) {
		if strings.TrimSpace(line) != "" {
			last = line
		}
	}
	return last
}

func firstNonBlankLine(s string) string {
	for line := range splitLines(s) {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// splitLines yields the lines of s without their terminators.
func splitLines(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
		if start <= len(s)-1 {
			yield(s[start:])
		}
	}
}

// lineOpensConstruct reports whether line keeps a markdown construct
// open across the boundary.
func lineOpensConstruct(line string) bool {
	if len(line) > 0 && line[0] == '\t' {
		return true
	}
	if strings.HasPrefix(line, "    ") {
		return true
	}

	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return false
	}

	if trimmed[0] == '>' {
		return true
	}

	if isListItemMarker(trimmed) {
		return true
	}

	if strings.ContainsRune(line, '|') {
		return true
	}

	if isSetextUnderlineCandidate(trimmed) {
		return true
	}

	return false
}

func isListItemMarker(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	if c == '-' || c == '*' || c == '+' {
		if len(line) >= 2 && (line[1] == ' ' || line[1] == '\t') {
			return true
		}
		return false
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i > 9 {
		return false
	}
	if i >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	return line[i+1] == ' ' || line[i+1] == '\t'
}

func isSetextUnderlineCandidate(line string) bool {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) {
		return false
	}
	c := line[i]
	if c != '=' && c != '-' {
		return false
	}
	j := i
	for j < len(line) && line[j] == c {
		j++
	}
	for j < len(line) {
		if line[j] != ' ' && line[j] != '\t' {
			return false
		}
		j++
	}
	return j-i >= 1
}

func isHTMLBlockOpener(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	if len(rest) < 2 || rest[0] != '<' {
		return false
	}

	if strings.HasPrefix(rest, "<!--") {
		return true
	}
	if strings.HasPrefix(rest, "<?") {
		return true
	}
	if strings.HasPrefix(rest, "<![CDATA[") {
		return true
	}
	if len(rest) >= 3 && rest[1] == '!' && isASCIILetter(rest[2]) {
		return true
	}

	low := strings.ToLower(rest)
	for _, t := range []string{"<script", "<pre", "<style", "<textarea"} {
		if strings.HasPrefix(low, t) {
			next := byte(0)
			if len(low) > len(t) {
				next = low[len(t)]
			}
			if next == 0 || next == ' ' || next == '\t' || next == '>' {
				return true
			}
		}
	}

	j := 1
	if j < len(rest) && rest[j] == '/' {
		j++
	}
	if j >= len(rest) || !isASCIILetter(rest[j]) {
		return false
	}
	return true
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isLinkRefDefinition(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || line[i] != '[' {
		return false
	}
	i++
	labelStart := i
	for i < len(line) && line[i] != ']' {
		i++
	}
	if i >= len(line) || i == labelStart {
		return false
	}
	i++
	if i >= len(line) || line[i] != ':' {
		return false
	}
	i++
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i < len(line)
}
