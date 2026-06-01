package agent

import (
	"context"
	"fmt"
	"log/slog"

	"cece/internal/tool"
)

const defaultSubAgentMaxTurns = 200

// SubAgentConfig configures a sub-agent run.
type SubAgentConfig struct {
	Prompt            string
	Description       string
	SystemPromptExtra string   // appended to default system prompt
	Model             string   // empty = same as parent
	Tools             []string // empty = default tool set
	ProjectDir        string
	MaxTokens         int
	MaxTurns          int // 0 = default (200)
	ToolResultPolicy  ToolResultPolicy
}

// SubAgentResult holds the outcome of a sub-agent run.
type SubAgentResult struct {
	Content      string
	InputTokens  int
	OutputTokens int
	TurnsUsed    int
	HitMaxTurns  bool
}

// SubAgent runs an autonomous agent loop for a single delegated task.
// It has its own conversation history, tools, and system prompt,
// but shares the project directory and model client with the parent.
type SubAgent struct {
	client   ModelClient
	registry *tool.Registry
	config   SubAgentConfig
}

// NewSubAgent creates a SubAgent with the given client, registry, and config.
func NewSubAgent(client ModelClient, registry *tool.Registry, config SubAgentConfig) *SubAgent {
	return &SubAgent{client: client, registry: registry, config: config}
}

// Run executes the sub-agent loop until the task completes, hits maxTurns, or ctx is cancelled.
// Internal events (tool exec deltas, thinking, etc.) are not emitted to the parent event channel —
// only fatal errors are logged. The caller should emit SubAgentStarted/Completed events.
func (sa *SubAgent) Run(ctx context.Context) SubAgentResult {
	systemPrompt := buildSubAgentSystemPrompt(sa.config)
	messages := []Message{{Role: UserRole, Content: sa.config.Prompt}}

	streamer := NewModelStreamer(sa.client, sa.registry, func(int) {})
	toolExecutor := NewToolExecutor(sa.registry, nil, nil, sa.config.ToolResultPolicy, nil)

	maxTurns := sa.config.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultSubAgentMaxTurns
	}

	var totalInput, totalOutput, turns int

	for turns < maxTurns {
		select {
		case <-ctx.Done():
			return SubAgentResult{
				Content:      fmt.Sprintf("sub-agent cancelled: %v", ctx.Err()),
				InputTokens:  totalInput,
				OutputTokens: totalOutput,
				TurnsUsed:    turns,
			}
		default:
		}

		resp, err := streamer.Stream(ctx, ModelStreamRequest{
			Messages:  messages,
			System:    systemPrompt,
			MaxTokens: sa.config.MaxTokens,
			Reason:    "subagent",
		}, nil) // nil events: don't stream internal details to UI

		if err != nil {
			// If it's a recoverable provider error, surface as text and let the model self-correct
			if isRecoverableProviderError(err) {
				slog.Warn("sub-agent recoverable error, surfacing to model", "error", err)
				errText := fmt.Sprintf("[provider error: %v]", err)
				if isContextTooLongError(err.Error()) {
					errText = fmt.Sprintf("[Context Window Exceeded] %v — the parent agent should compact before retrying.", err)
				}
				messages = append(messages,
					Message{Role: AssistantRole, Content: ""},
					Message{Role: UserRole, Content: errText},
				)
				turns++
				continue
			}
			return SubAgentResult{
				Content:      fmt.Sprintf("sub-agent error: %v", err),
				InputTokens:  totalInput,
				OutputTokens: totalOutput,
				TurnsUsed:    turns,
			}
		}

		totalInput += resp.inputTokens
		totalOutput += resp.outputTokens

		assistant := assistantMessageFromResponse(resp)
		messages = append(messages, assistant)

		// No tool calls — task is done.
		if resp.stopReason != "tool_use" || len(resp.toolCalls) == 0 {
			return SubAgentResult{
				Content:      resp.textContent,
				InputTokens:  totalInput,
				OutputTokens: totalOutput,
				TurnsUsed:    turns,
			}
		}

		// Execute tools directly (no interaction gate — sub-agent always yolo).
		toolResults := toolExecutor.ExecuteBatch(ctx, resp.toolCalls, nil) // nil events: silent
		resultMsg := Message{Role: UserRole, ContentBlocks: toolResults}
		messages = append(messages, resultMsg)

		turns++
	}

	// Hit max turns — return partial result with marker.
	return SubAgentResult{
		Content:      "<max_turns_reached>Sub-agent reached the maximum number of turns without completing. Partial results may be incomplete.</max_turns_reached>",
		InputTokens:  totalInput,
		OutputTokens: totalOutput,
		TurnsUsed:    turns,
		HitMaxTurns:  true,
	}
}

// SubAgentToolNames returns the default tool names available to sub-agents.
// This excludes tools that should not be available in sub-agents.
func SubAgentToolNames() []string {
	return []string{
		"Bash",
		"Read",
		"Write",
		"Edit",
		"Grep",
		"Glob",
		"WebFetch",
	}
}

// SubAgentExcludedToolNames returns tool names that must NOT be available to sub-agents.
func SubAgentExcludedToolNames() []string {
	return []string{
		"Agent",
		"Todo",
		"EnterPlanMode",
		"ExitPlanMode",
		"AskUserQuestion",
		"Skill",
		"Compact",
	}
}

// buildSubAgentSystemPrompt builds the default system prompt for a sub-agent.
func buildSubAgentSystemPrompt(config SubAgentConfig) SystemPrompt {
	base := fmt.Sprintf(`You are an autonomous sub-agent for Cece. Your job is to complete the task described below thoroughly and report back with a concise summary.

Guidelines:
- Complete the task fully — don't leave it half-done, but don't gold-plate either.
- Use tools proactively: read files, search code, run commands, edit files.
- Be thorough in research: check multiple locations, consider different naming conventions.
- For implementation: make targeted changes, run tests to verify.
- Report back with actionable findings — the main agent will synthesize your results.
- If you encounter errors, investigate and attempt to fix them before reporting failure.
- NEVER create documentation files unless explicitly instructed.
- NEVER spawn further sub-agents (the Agent tool is not available to you).

Working directory: %s`, config.ProjectDir)

	text := base
	if config.SystemPromptExtra != "" {
		text += "\n\n" + config.SystemPromptExtra
	}

	return SystemPrompt{
		Blocks: []SystemBlock{
			{Text: text},
		},
	}
}
