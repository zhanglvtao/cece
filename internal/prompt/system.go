package prompt

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var defaultSystemPrompt string

// FormatStableSystemPrompt returns the embedded stable (cacheable) system prompt.
// repoRoot is kept for API compatibility; project-specific instructions belong in
// the session layer via AGENTS.md or CLAUDE.md, not in a stable prompt override.
func FormatStableSystemPrompt(repoRoot string) string {
	return strings.TrimSpace(defaultSystemPrompt)
}

// FormatSubAgentSystemPrompt returns the system prompt for a sub-agent.
// It starts with the default stable prompt and appends any extra instructions.
func FormatSubAgentSystemPrompt(repoRoot string, systemPromptExtra string) string {
	base := FormatStableSystemPrompt(repoRoot)
	if systemPromptExtra != "" {
		return base + "\n\n" + systemPromptExtra
	}
	return base
}
