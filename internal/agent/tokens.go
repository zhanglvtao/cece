package agent

import (
	"encoding/json"

	"cece/internal/prompt"
	"cece/internal/tool"
)

// estimateRequestTokens approximates the number of input tokens in a model
// request before it is sent. The estimate covers system prompt, conversation
// messages (text + tool_use input + tool_result content) and tool definitions.
//
// It uses the heuristic estimator from the prompt package — cheap and
// synchronous, intended for pre-flight UI display, not billing.
func estimateRequestTokens(system SystemPrompt, messages []Message, tools []tool.Definition) int {
	total := 0

	for _, b := range system.Blocks {
		total += prompt.EstimateTokens(b.Text)
	}

	for _, m := range messages {
		if m.Content != "" {
			total += prompt.EstimateTokens(m.Content)
		}
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case ApiTextContentType:
				total += prompt.EstimateTokens(cb.Text)
			case ApiToolUseContentType:
				if cb.ToolUse != nil {
					total += prompt.EstimateTokens(cb.ToolUse.Name)
					total += prompt.EstimateTokens(string(cb.ToolUse.Input))
				}
			case ApiToolResultContentType:
				if cb.ToolResult != nil {
					total += prompt.EstimateTokens(cb.ToolResult.Content)
				}
			}
		}
	}

	for _, t := range tools {
		total += prompt.EstimateTokens(t.Name)
		total += prompt.EstimateTokens(t.Description)
		if t.InputSchema != nil {
			if raw, err := json.Marshal(t.InputSchema); err == nil {
				total += prompt.EstimateTokens(string(raw))
			}
		}
	}

	return total
}
