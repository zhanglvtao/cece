package ui

import (
	"fmt"
	"strings"
)

type slashSpec struct {
	Command  string // e.g. "/model"
	Query    string // e.g. "model"
	Args     string
	Active   bool
	HasArgs  bool
	StartIdx int // byte index of "/" in input
}

func (s slashSpec) Valid() bool {
	return s.Active && s.Command != ""
}

// parseSlashSpec finds the last "/" preceded by whitespace (or at start of
// input) and extracts the slash command word. Works at any input position.
func parseSlashSpec(input string) slashSpec {
	// Find the last "/" at position 0 or preceded by whitespace.
	slashIdx := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i] == '/' {
			if i == 0 || isSpace(input[i-1]) {
				slashIdx = i
				break
			}
		}
	}
	if slashIdx < 0 {
		return slashSpec{}
	}
	// Bare "/" at end of input: active with empty query (popup shows all commands).
	if slashIdx+1 >= len(input) {
		return slashSpec{
			Command:  input[slashIdx:],
			Query:    "",
			Active:   true,
			StartIdx: slashIdx,
		}
	}
	if isSpace(input[slashIdx+1]) || input[slashIdx+1] == '/' {
		return slashSpec{}
	}

	// Extract word from "/" to whitespace or end.
	wordEnd := len(input)
	for i := slashIdx + 1; i < len(input); i++ {
		if isSpace(input[i]) {
			wordEnd = i
			break
		}
	}
	word := input[slashIdx:wordEnd]

	args := ""
	hasArgs := false
	if wordEnd < len(input) {
		args = strings.TrimSpace(input[wordEnd:])
		hasArgs = true
	}

	return slashSpec{
		Command:  word,
		Query:    word[1:], // strip leading "/"
		Args:     args,
		Active:   true,
		HasArgs:  hasArgs,
		StartIdx: slashIdx,
	}
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n'
}

func formatSlashUnknown(command string) string {
	return fmt.Sprintf("Unknown command: %s", command)
}

func slashHighlightValue(_ Styles, input string) string { return input }
