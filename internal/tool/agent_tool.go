package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

const AgentToolName = "Agent"

// AgentHandler provides the callback for the Agent tool to run sub-agents.
// Using function fields avoids circular imports between tool and agent packages.
type AgentHandler struct {
	// RunSubAgent creates and runs a sub-agent with the given config.
	// Returns the result text, input tokens, output tokens, turns used, whether max turns was hit, and error.
	RunSubAgent func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error)
}

// AgentSubAgentConfig is the config passed from the Agent tool to the engine.
type AgentSubAgentConfig struct {
	Prompt            string
	Description       string
	SubAgentType      string
	Model             string
	Tools             []string
	SystemPromptExtra string
	MaxTurns          int
}

// AgentSubAgentResult is the result of a sub-agent run.
type AgentSubAgentResult struct {
	Content      string
	InputTokens  int
	OutputTokens int
	TurnsUsed    int
	HitMaxTurns  bool
	Cancelled    bool
	Err          string
}

type agentTool struct {
	handler *AgentHandler
}

// NewAgent creates an Agent tool with the given handler.
func NewAgent(handler *AgentHandler) Tool {
	return agentTool{handler: handler}
}

func (agentTool) Effect() Effect { return EffectMode }

func (agentTool) Info() Definition {
	return Definition{
		Name:        AgentToolName,
		Description: "Launch a sub-agent to handle a complex, multi-step task autonomously. Multiple Agent calls in a single response will run in parallel. The sub-agent has its own conversation history and tool set, but shares the project directory. It cannot spawn further sub-agents.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"prompt"},
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "The task for the sub-agent to perform. Brief it like a smart colleague who just walked into the room — it hasn't seen this conversation, doesn't know what you've tried. Provide enough context for it to work autonomously.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "3-5 word summary of what this agent will do, for UI display.",
				},
				"subagent_type": map[string]any{
					"type":        "string",
					"description": "Predefined agent type (e.g. 'explore', 'coder'). Determines default tools and system prompt. Defaults to 'general'.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Model to use for this sub-agent. Defaults to the same model as the parent agent.",
				},
				"tools": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Override the tool list for this sub-agent. By default, all parent tools are available except Agent (prevents nesting). Available tools: Bash, Read, Write, Edit, Grep, Glob, WebFetch, Compact, Skill, Todo, EnterPlanMode, ExitPlanMode, AskUserQuestion.",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "Additional context appended to the sub-agent's system prompt. Use this to pass project-specific instructions, coding conventions, or constraints.",
				},
				"max_turns": map[string]any{
					"type":        "integer",
					"description": "Maximum number of agentic iterations. The sub-agent stops naturally when finished. Only set this to limit resource usage. Default: no limit.",
				},
			},
		},
	}
}

type agentParams struct {
	Prompt       string   `json:"prompt"`
	Description  string   `json:"description,omitempty"`
	SubAgentType string   `json:"subagent_type,omitempty"`
	Model        string   `json:"model,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
}

func (t agentTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil || t.handler.RunSubAgent == nil {
		return Result{Content: "agent handler is not configured", IsError: true}
	}

	var p agentParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	if p.Prompt == "" {
		return Result{Content: "prompt is required", IsError: true}
	}

	description := p.Description
	if description == "" {
		description = "Agent task"
	}

	emitter.Emit(fmt.Sprintf("Launching sub-agent: %s\n", description))

	result, err := t.handler.RunSubAgent(ctx, AgentSubAgentConfig{
		Prompt:            p.Prompt,
		Description:       description,
		SubAgentType:      p.SubAgentType,
		Model:             p.Model,
		Tools:             p.Tools,
		SystemPromptExtra: p.SystemPrompt,
		MaxTurns:          p.MaxTurns,
	}, emitter)

	if err != nil {
		slog.Error("sub-agent failed", "description", description, "error", err)
		return Result{Content: fmt.Sprintf("Sub-agent failed: %v", err), IsError: true}
	}
	if result.Cancelled || result.Err != "" {
		return Result{Content: result.Content, IsError: true}
	}

	// Build result with metadata
	var b strings.Builder
	b.WriteString(result.Content)

	if result.HitMaxTurns {
		b.WriteString("\n\n[Sub-agent hit max turns limit]")
	}

	b.WriteString(fmt.Sprintf("\n\n---\nTokens: %dK in / %dK out | Turns: %d",
		(result.InputTokens+999)/1000,
		(result.OutputTokens+999)/1000,
		result.TurnsUsed,
	))

	return Result{Content: b.String()}
}
