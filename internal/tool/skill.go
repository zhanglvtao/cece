package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"cece/internal/skill"
)

const SkillToolName = "Skill"

type skillParams struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
}

type skillTool struct {
	store *skill.Store
}

// NewSkillTool creates a tool that loads a skill's instructions by name.
func NewSkillTool(store *skill.Store) Tool {
	return skillTool{store: store}
}

func (skillTool) Effect() Effect { return EffectRead }

func (skillTool) Info() Definition {
	return Definition{
		Name:        SkillToolName,
		Description: "Load a skill's instructions by name. Skills provide specialized capabilities and domain knowledge. Available skills are listed in <available_skills>. When a skill matches the user's request, invoke this tool BEFORE generating any other response. Do not invoke a skill that is already loaded in the current conversation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The skill name to load (e.g., \"cece-config\")",
				},
				"args": map[string]any{
					"type":        "string",
					"description": "Optional arguments to pass to the skill, appended as additional context",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t skillTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p skillParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}
	}

	if t.store == nil {
		return Result{Content: "skill store not configured", IsError: true}
	}

	s, ok := t.store.Get(p.Name)
	if !ok {
		return Result{
			Content: fmt.Sprintf("Unknown skill: %s. Use one of the skills listed in <available_skills>.", p.Name),
			IsError: true,
		}
	}

	return Result{Content: skill.FormatToolResult(s, p.Args)}
}
