package tool

import (
	"context"
	"encoding/json"
)

const AskUserQuestionToolName = "AskUserQuestion"

// ── Shared types (used by Event + UI) ──────────────────────────────────────

// Question represents a single question from the LLM to the user.
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Options     []QuestionOption `json:"options"`
	Preview     string           `json:"preview,omitempty"`
}

// QuestionOption is a single choice for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionAnswer is the user's response to a question.
type QuestionAnswer struct {
	Question string   `json:"question"`
	Selected []string `json:"selected,omitempty"`
	Custom   string   `json:"custom,omitempty"`
}

// ── Tool ───────────────────────────────────────────────────────────────────

type askUserQuestionTool struct{}

func NewAskUserQuestion() Tool {
	return askUserQuestionTool{}
}

func (askUserQuestionTool) Effect() Effect { return EffectMode }

func (askUserQuestionTool) Info() Definition {
	return Definition{
		Name:        AskUserQuestionToolName,
		Description: "Ask the user questions to gather requirements, clarify ambiguities, or make decisions. Supports single-select, multi-select, and free-text answers.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"questions"},
			"properties": map[string]any{
				"questions": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items": map[string]any{
						"type":     "object",
						"required": []any{"question", "options"},
						"properties": map[string]any{
							"question": map[string]any{
								"type":        "string",
								"description": "The question to ask the user. Clear and specific, ending with a question mark.",
							},
							"header": map[string]any{
								"type":        "string",
								"description": "Short label for this question (max 12 chars). Examples: 'Auth method', 'Library'.",
							},
							"multiSelect": map[string]any{
								"type":        "boolean",
								"description": "Set to true to allow multiple selections.",
							},
							"options": map[string]any{
								"type":     "array",
								"minItems": 2,
								"items": map[string]any{
									"type":     "object",
									"required": []any{"label"},
									"properties": map[string]any{
										"label": map[string]any{
											"type":        "string",
											"description": "Display text for this option (1-5 words).",
										},
										"description": map[string]any{
											"type":        "string",
											"description": "Explanation of what this option means.",
										},
									},
								},
								"preview": map[string]any{
									"type":        "string",
									"description": "Optional text preview to display above the options (e.g. ASCII art, diagrams, code).",
								},
							},
						},
					},
				},
			},
		},
	}
}

func (t askUserQuestionTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	// Actual answers are injected by Runtime during executeTools.
	// This Run() should never be called directly; if it is, return a placeholder.
	var parsed struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return Result{Content: "AskUserQuestion failed to parse input — answers should be injected by the runtime.", IsError: true}
	}
	// Return placeholder: runtime replaces this before it reaches the model
	var qs []string
	for _, q := range parsed.Questions {
		qs = append(qs, q.Question)
	}
	return Result{Content: "AskUserQuestion pending — user has not answered yet."}
}