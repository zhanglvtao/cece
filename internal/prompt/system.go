package prompt

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var stableSystemPrompt string

func FormatStableSystemPrompt() string {
	return strings.TrimSpace(stableSystemPrompt)
}
