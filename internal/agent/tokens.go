package agent

import (
	"encoding/json"

	"cece/internal/prompt"
	"cece/internal/tool"
)

// Structural token overhead per API content block type.
// These compensate for JSON wrapping the API adds around each block
// (type field, id, tool_use_id, boundaries, etc.) that the raw text
// estimate does not account for.
const (
	overheadPerMessage    = 5 // role + message boundaries
	overheadTextBlock     = 3 // type="text" + boundaries
	overheadThinkingBlock = 4 // type="thinking" + signature
	overheadToolUseBlock  = 8 // type + id + name + input wrapper
	overheadToolResultBlock = 6 // type + tool_use_id + is_error
	overheadSystemPrompt  = 4 // system message boundaries
	overheadPerToolDef    = 3 // tool definition wrapper
)

// EstimateRequestTokens approximates the number of input tokens in a model
// request before it is sent. The estimate covers system prompt, conversation
// messages (text + tool_use input + tool_result content) and tool definitions.
//
// It uses tiktoken BPE for precise text counting plus structural overhead
// constants to account for API message formatting that raw text cannot capture.
func EstimateRequestTokens(system SystemPrompt, messages []Message, tools []tool.Definition) int {
	total := 0

	for _, b := range system.Blocks {
		total += prompt.PreciseEstimateTokens(b.Text)
	}
	if len(system.Blocks) > 0 {
		total += overheadSystemPrompt
	}

	for _, m := range messages {
		total += overheadPerMessage
		if m.Content != "" {
			total += prompt.PreciseEstimateTokens(m.Content)
		}
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case ApiTextContentType:
				total += overheadTextBlock
				total += prompt.PreciseEstimateTokens(cb.Text)
			case ApiThinkingContentType:
				total += overheadThinkingBlock
				if cb.Thinking != nil {
					total += prompt.PreciseEstimateTokens(cb.Thinking.Text)
				}
			case ApiToolUseContentType:
				total += overheadToolUseBlock
				if cb.ToolUse != nil {
					total += prompt.PreciseEstimateTokens(cb.ToolUse.Name)
					total += prompt.PreciseEstimateTokens(string(cb.ToolUse.Input))
				}
			case ApiToolResultContentType:
				total += overheadToolResultBlock
				if cb.ToolResult != nil {
					total += prompt.PreciseEstimateTokens(cb.ToolResult.Content)
				}
			}
		}
	}

	for _, t := range tools {
		total += overheadPerToolDef
		total += prompt.PreciseEstimateTokens(t.Name)
		total += prompt.PreciseEstimateTokens(t.Description)
		if t.InputSchema != nil {
			if raw, err := json.Marshal(t.InputSchema); err == nil {
				total += prompt.PreciseEstimateTokens(string(raw))
			}
		}
	}

	return total
}
