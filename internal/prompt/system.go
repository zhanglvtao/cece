package prompt

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var defaultSystemPrompt string

const interactiveBuiltInAgentsGuidance = `Use Agent for independent subtasks, parallelizable work, long-running investigations, code changes, reviews, or background execution.

built-in agents:
- research: search, read, summarize, investigate
- coding: implement, fix, update code, add focused tests
- review: inspect changes, verify behavior, find risks
- execution: run, wait, follow up, drive background progress

When starting an agent, choose the correct agent_type explicitly.`

// FormatStableSystemPrompt returns the embedded stable (cacheable) system prompt.
// repoRoot is kept for API compatibility; project-specific instructions belong in
// the session layer via AGENTS.md or CLAUDE.md, not in a stable prompt override.
func FormatStableSystemPrompt(repoRoot string) string {
	return strings.TrimSpace(defaultSystemPrompt)
}

func FormatInteractiveSystemPrompt(repoRoot string) string {
	base := FormatStableSystemPrompt(repoRoot)
	return base + "\n\n" + strings.TrimSpace(interactiveBuiltInAgentsGuidance)
}

func subAgentProfileGuidance(profile string) string {
	switch strings.TrimSpace(profile) {
	case "research":
		return "Focus on searching, reading, and summarizing. Collect evidence before concluding."
	case "coding":
		return "Focus on implementation work. Keep code changes focused and avoid drifting into open-ended research."
	case "review":
		return "Focus on inspection and verification. Inspect for risks and omissions before approving conclusions."
	case "execution":
		return "Focus on drive progress and report status clearly. Wait when needed."
	default:
		return ""
	}
}

// FormatSubAgentSystemPrompt returns the system prompt for a sub-agent.
// It starts with the default stable prompt and appends profile guidance and any extra instructions.
func FormatSubAgentSystemPrompt(repoRoot string, profile string, systemPromptExtra string) string {
	parts := []string{FormatStableSystemPrompt(repoRoot)}
	if guidance := strings.TrimSpace(subAgentProfileGuidance(profile)); guidance != "" {
		parts = append(parts, guidance)
	}
	if extra := strings.TrimSpace(systemPromptExtra); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, "\n\n")
}
