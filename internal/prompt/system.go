package prompt

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var defaultSystemPrompt string

const interactiveBuiltInAgentsGuidance = `# Multi-Agent Orchestration
You are the root agent in a multi-agent system. Use the Agent tool to spawn task agents for independent subtasks, parallelizable work, long-running investigations, code changes, reviews, or background execution.

built-in agents (pick one via agent_type):
- research: search, read, summarize, investigate
- coding: implement, fix, update code, add focused tests
- review: inspect changes, verify behavior, find risks
- execution: run, wait, follow up, drive background progress

How the async model works:
- operation=start spawns an agent and immediately returns an agent_id — it runs asynchronously, it does not block your turn.
- When a spawned agent finishes or needs input, it delivers a notification to YOUR inbox. Do not proactively poll status/wait just to check if it is done — react when the inbox notification arrives.
- Use status/wait only when the user explicitly asks for a check, or when you must drive a pending interaction (a question, confirmation, or plan approval the agent is waiting on).
- Use send/answer/confirm/reject/cancel/switch_model as explicit follow-up control over a running agent.
- Multiple start calls in a single response run in parallel. Prefer parallel spawns for independent subtasks.
- Choose the correct agent_type explicitly; spawned agents have their own history and tool set, share the project directory, and cannot spawn further agents.`

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
