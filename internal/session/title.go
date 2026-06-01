package session

import (
	"strings"
	"unicode"
)

const maxTitleLength = 80
const maxPreviewLength = 60

// GenerateTitle produces a session title from the first user message.
// Truncates to maxTitleLength chars at word boundary.
func GenerateTitle(firstUserMessage string) string {
	s := normalizeMessage(firstUserMessage)
	if s == "" {
		return "New Session"
	}
	return truncateAtWord(s, maxTitleLength)
}

// UpdateTitle appends the latest user message prefix to the existing title.
// Format: "original title — latest message prefix"
func UpdateTitle(currentTitle string, latestUserMessage string) string {
	prefix := normalizeMessage(latestUserMessage)
	if prefix == "" {
		return currentTitle
	}

	// Strip any previous " — suffix" from the current title
	if idx := strings.LastIndex(currentTitle, " — "); idx > 0 {
		currentTitle = currentTitle[:idx]
	}

	prefix = truncateAtWord(prefix, 30)
	updated := currentTitle + " — " + prefix

	// If total too long, trim the base title
	runes := []rune(updated)
	if len(runes) > maxTitleLength {
		// Keep the suffix, trim the base
		suffixRunes := []rune(" — " + prefix)
		baseBudget := maxTitleLength - len(suffixRunes)
		if baseBudget < 10 {
			baseBudget = 10
		}
		baseRunes := []rune(currentTitle)
		if len(baseRunes) > baseBudget {
			baseRunes = baseRunes[:baseBudget]
			// Trim trailing space/punctuation
			for len(baseRunes) > 0 && (unicode.IsSpace(baseRunes[len(baseRunes)-1]) || baseRunes[len(baseRunes)-1] == '…') {
				baseRunes = baseRunes[:len(baseRunes)-1]
			}
			baseRunes = append(baseRunes, '…')
		}
		updated = string(baseRunes) + " — " + prefix
	}

	return updated
}

func normalizeMessage(msg string) string {
	s := strings.TrimSpace(msg)

	// Strip slash commands
	if strings.HasPrefix(s, "/") {
		if idx := strings.IndexByte(s, ' '); idx > 0 {
			s = s[idx+1:]
		} else {
			return ""
		}
	}

	return strings.TrimSpace(s)
}

// truncatePreview truncates a message for use as a session preview.
func truncatePreview(msg string) string {
	s := strings.TrimSpace(msg)
	// Collapse newlines for single-line preview
	s = strings.Join(strings.Fields(s), " ")
	return truncateAtWord(s, maxPreviewLength)
}

func truncateAtWord(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	// Find last space before limit
	cut := limit
	for i := limit - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	if cut == 0 {
		cut = limit
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}
