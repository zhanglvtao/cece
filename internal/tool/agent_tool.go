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
type AgentHandler struct {
	RunSubAgent func(ctx context.Context, config AgentSubAgentConfig, emitter Emitter) (AgentSubAgentResult, error)
}

// AgentSubAgentConfig is the config passed from the Agent tool to the engine.
type AgentSubAgentConfig struct {
	Operation         string
	AgentID           string
	Prompt            string
	Input             string
	Answers           []QuestionAnswer
	Description       string
	SubAgentType      string
	Model             string
	Tools             []string
	SystemPromptExtra string
	MaxTurns          int
	TimeoutMS         int
}

// AgentSubAgentResult is the result of a sub-agent run.
type AgentSubAgentResult struct {
	AgentID      string
	SessionID    string
	Status       string
	Content      string
	InputTokens  int
	OutputTokens int
	TurnsUsed    int
	HitMaxTurns  bool
	Cancelled    bool
	Err          string

	// Artifact fields — populated when the result was persisted as an artifact.
	ResultPath            string
	ContentFullLength     int
	ContentReturnedLength int
	ContentTruncated      bool
}

type agentTool struct {
	handler       *AgentHandler
	modelChoices  []string
	modelProvider func() []string
}

type AgentOption func(*agentTool)

func WithAgentModels(models []string) AgentOption {
	return func(t *agentTool) {
		t.modelChoices = uniqueAgentModels(models)
	}
}

func WithAgentModelProvider(provider func() []string) AgentOption {
	return func(t *agentTool) {
		t.modelProvider = provider
	}
}

// NewAgent creates an Agent tool with the given handler.
func NewAgent(handler *AgentHandler, opts ...AgentOption) Tool {
	t := agentTool{handler: handler}
	for _, opt := range opts {
		if opt != nil {
			opt(&t)
		}
	}
	return t
}

func (agentTool) Effect() Effect { return EffectMode }

func (t agentTool) Info() Definition {
	choices := t.agentModelChoices()
	description := "Model for this sub-agent. Optional: omit to use the current/default model."
	if len(choices) > 0 {
		description += " Available models: " + strings.Join(choices, ", ") + "."
	}
	modelSchema := map[string]any{
		"type":        "string",
		"description": description,
	}
	if len(choices) > 0 {
		modelSchema["enum"] = choices
	}
	return Definition{
		Name:        AgentToolName,
		Description: "Start and control worker agents asynchronously. Use operation=start to launch a worker and immediately receive an agent_id, then use status/wait/send/answer/confirm/reject/cancel to drive it. Multiple Agent start calls in a single response can run in parallel. Workers have their own conversation history and tool set, share the project directory, and cannot spawn further agents.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"description": "Operation: start (default), status, wait, send, answer, confirm, reject, switch_model, or cancel.",
				},
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Target agent ID for non-start operations.",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "Additional semantic input for send operation.",
				},
				"answers": map[string]any{
					"type":        "array",
					"description": "Answers for answer operation.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"question": map[string]any{"type": "string"},
							"selected": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"custom":   map[string]any{"type": "string"},
						},
					},
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "The task for the sub-agent to perform.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "3-5 word summary for UI display.",
				},
				"subagent_type": map[string]any{
					"type":        "string",
					"description": "Predefined agent type.",
				},
				"model": modelSchema,
				"tools": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Override tool list.",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "Additional system prompt context.",
				},
				"max_turns": map[string]any{
					"type":        "integer",
					"description": "Max agentic iterations.",
				},
				"timeout_ms": map[string]any{
					"type":        "integer",
					"description": "Max wait time in milliseconds for wait operation (default 30000).",
				},
			},
		},
	}
}

type agentParams struct {
	Operation    string           `json:"operation,omitempty"`
	AgentID      string           `json:"agent_id,omitempty"`
	Prompt       string           `json:"prompt,omitempty"`
	Input        string           `json:"input,omitempty"`
	Answers      []QuestionAnswer `json:"answers,omitempty"`
	Description  string           `json:"description,omitempty"`
	SubAgentType string           `json:"subagent_type,omitempty"`
	Model        string           `json:"model,omitempty"`
	Tools        []string         `json:"tools,omitempty"`
	SystemPrompt string           `json:"system_prompt,omitempty"`
	MaxTurns     int              `json:"max_turns,omitempty"`
	TimeoutMS    int              `json:"timeout_ms,omitempty"`
}

func (t agentTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil || t.handler.RunSubAgent == nil {
		return Result{Content: "agent handler is not configured", IsError: true}
	}

	var p agentParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	operation := strings.TrimSpace(p.Operation)
	if operation == "" {
		operation = "start"
	}

	slog.Info("Agent tool called",
		"operation", operation,
		"agentID", p.AgentID,
		"description", p.Description,
		"model", p.Model,
		"maxTurns", p.MaxTurns,
		"timeoutMS", p.TimeoutMS,
	)

	if operation == "start" && p.Prompt == "" {
		return Result{Content: "prompt is required for start operation", IsError: true}
	}
	if operation != "start" && p.AgentID == "" {
		return Result{Content: "agent_id is required for non-start operations", IsError: true}
	}

	description := p.Description
	if description == "" {
		description = "Agent task"
	}

	if operation == "start" {
		if emitter != nil {
			emitter.Emit(fmt.Sprintf("Launching sub-agent: %s\n", description))
		}
	}

	result, err := t.handler.RunSubAgent(ctx, AgentSubAgentConfig{
		Operation:         operation,
		AgentID:           p.AgentID,
		Prompt:            p.Prompt,
		Input:             p.Input,
		Answers:           p.Answers,
		Description:       description,
		SubAgentType:      p.SubAgentType,
		Model:             p.Model,
		Tools:             p.Tools,
		SystemPromptExtra: p.SystemPrompt,
		MaxTurns:          p.MaxTurns,
		TimeoutMS:         p.TimeoutMS,
	}, emitter)

	if err != nil {
		slog.Error("sub-agent failed", "description", description, "error", err)
		return Result{Content: fmt.Sprintf("Sub-agent failed: %v", err), IsError: true}
	}
	if result.Cancelled || result.Err != "" {
		slog.Warn("sub-agent cancelled or errored",
			"agentID", result.AgentID,
			"cancelled", result.Cancelled,
			"err", result.Err,
		)
		return Result{Content: result.Content, IsError: true}
	}

	if result.Status != "" && result.Status != "completed" {
		var b strings.Builder
		b.WriteString(result.Content)
		if result.AgentID != "" {
			b.WriteString(fmt.Sprintf("\n\nAgent: %s", result.AgentID))
			if result.SessionID != "" {
				b.WriteString(fmt.Sprintf(" | Session: %s", result.SessionID))
			}
		}
		b.WriteString(fmt.Sprintf("\nStatus: %s", result.Status))
		return Result{Content: b.String()}
	}

	var b strings.Builder
	b.WriteString(result.Content)

	if result.HitMaxTurns {
		b.WriteString("\n\n[Sub-agent hit max turns limit]")
	}

	if result.ContentTruncated {
		b.WriteString(fmt.Sprintf("\n\nPreview truncated: %d / %d chars", result.ContentReturnedLength, result.ContentFullLength))
		if result.ResultPath != "" {
			b.WriteString(fmt.Sprintf("\nResult artifact: %s", result.ResultPath))
			b.WriteString("\nUse Read with this path to inspect the full result.")
		}
	} else if result.ResultPath != "" {
		b.WriteString(fmt.Sprintf("\n\nResult artifact: %s", result.ResultPath))
	}

	if result.AgentID != "" {
		b.WriteString(fmt.Sprintf("\n\nAgent: %s", result.AgentID))
		if result.SessionID != "" {
			b.WriteString(fmt.Sprintf(" | Session: %s", result.SessionID))
		}
	}
	b.WriteString(fmt.Sprintf("\n\n---\nTokens: %dK in / %dK out | Turns: %d",
		(result.InputTokens+999)/1000,
		(result.OutputTokens+999)/1000,
		result.TurnsUsed,
	))

	slog.Info("subagent: result returned to parent",
		"agentID", result.AgentID,
		"sessionID", result.SessionID,
		"truncated", result.ContentTruncated,
		"resultPath", result.ResultPath,
		"turns", result.TurnsUsed,
	)

	return Result{Content: b.String()}
}

func (t agentTool) agentModelChoices() []string {
	models := append([]string(nil), t.modelChoices...)
	if t.modelProvider != nil {
		models = append(models, t.modelProvider()...)
	}
	return uniqueAgentModels(models)
}

func uniqueAgentModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}
