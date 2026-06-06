package prompt

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

//go:embed system.md
var defaultSystemPrompt string

// FormatStableSystemPrompt returns the stable (cacheable) system prompt.
// If repoRoot/SYSTEM.md exists, it is used as the full system prompt
// (complete override). Otherwise the embedded default is returned.
func FormatStableSystemPrompt(repoRoot string) string {
	if repoRoot != "" {
		path := filepath.Join(repoRoot, "SYSTEM.md")
		data, err := os.ReadFile(path)
		if err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content
			}
		}
	}
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
