package openai

import (
	"strings"

	"cece/internal/chat"
)

type ChatCompletionRequest struct {
	Model     string       `json:"model"`
	Messages  []OAIMessage `json:"messages"`
	MaxTokens int          `json:"max_tokens,omitempty"`
	Stream    bool         `json:"stream"`
	Tools     []OAITool    `json:"tools,omitempty"`
}

type OAIMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []OAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type OAIToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function OAIFunction `json:"function"`
}

type OAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// SerializeMessages converts internal messages + system prompt into OpenAI ChatCompletion format.
func SerializeMessages(messages []chat.Message, system chat.SystemPrompt) []OAIMessage {
	var result []OAIMessage

	if len(system.Blocks) > 0 {
		var sb strings.Builder
		for i, block := range system.Blocks {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(block.Text)
		}
		result = append(result, OAIMessage{Role: "system", Content: sb.String()})
	}

	for _, m := range messages {
		result = append(result, serializeMessageExpanded(m)...)
	}

	return result
}

// serializeMessageExpanded returns 1+ OAIMessages (multi-tool-result expansion).
func serializeMessageExpanded(m chat.Message) []OAIMessage {
	// User role with tool_result blocks: expand each into separate "tool" role message
	if m.Role == chat.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var msgs []OAIMessage
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					msgs = append(msgs, OAIMessage{
						Role:       "tool",
						ToolCallID: tr.ToolUseID,
						Content:    tr.Content,
					})
				}
			}
			return msgs
		}
	}

	return []OAIMessage{serializeMessage(m)}
}

func serializeMessage(m chat.Message) OAIMessage {
	if len(m.ContentBlocks) > 0 && m.Role == chat.AssistantRole {
		msg := OAIMessage{Role: "assistant"}
		var textParts []string
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case chat.ApiTextContentType:
				textParts = append(textParts, cb.Text)
			case chat.ApiToolUseContentType:
				if cb.ToolUse != nil {
					msg.ToolCalls = append(msg.ToolCalls, OAIToolCall{
						ID:   cb.ToolUse.ID,
						Type: "function",
						Function: OAIFunction{
							Name:      cb.ToolUse.Name,
							Arguments: string(cb.ToolUse.Input),
						},
					})
				}
			// ApiThinkingContentType: dropped
			}
		}
		if len(textParts) > 0 {
			msg.Content = strings.Join(textParts, "")
		}
		return msg
	}

	return OAIMessage{
		Role:    string(m.Role),
		Content: m.Content,
	}
}
