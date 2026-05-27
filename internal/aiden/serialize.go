package aiden

import (
	"strings"

	"cece/internal/chat"
)

type ChatCompletionRequest struct {
	Model     string      `json:"model"`
	Messages  []AidenMsg  `json:"messages"`
	MaxTokens int         `json:"max_tokens,omitempty"`
	Stream    bool        `json:"stream"`
	Tools     []AidenTool `json:"tools,omitempty"`
}

type AidenMsg struct {
	Role       string          `json:"role"`
	Content    any             `json:"content"`
	ToolCalls  []AidenToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type AidenContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AidenToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function AidenFunction `json:"function"`
}

type AidenFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func SerializeMessages(messages []chat.Message, system chat.SystemPrompt) []AidenMsg {
	var result []AidenMsg

	if len(system.Blocks) > 0 {
		var sb strings.Builder
		for i, block := range system.Blocks {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(block.Text)
		}
		s := sb.String()
		result = append(result, AidenMsg{Role: "system", Content: s})
	}

	for _, m := range messages {
		result = append(result, serializeMessageExpanded(m)...)
	}

	return result
}

func serializeMessageExpanded(m chat.Message) []AidenMsg {
	if m.Role == chat.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var msgs []AidenMsg
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					msgs = append(msgs, AidenMsg{
						Role:       "tool",
						ToolCallID: tr.ToolUseID,
						Content:    tr.Content,
					})
				}
			}
			return msgs
		}
	}

	return []AidenMsg{serializeMessage(m)}
}

func serializeMessage(m chat.Message) AidenMsg {
	if m.Role == chat.AssistantRole {
		msg := AidenMsg{
			Role: "assistant",
		}
		var textParts []string
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case chat.ApiTextContentType:
				textParts = append(textParts, cb.Text)
			case chat.ApiToolUseContentType:
				if cb.ToolUse != nil {
					msg.ToolCalls = append(msg.ToolCalls, AidenToolCall{
						ID:   cb.ToolUse.ID,
						Type: "function",
						Function: AidenFunction{
							Name:      cb.ToolUse.Name,
							Arguments: string(cb.ToolUse.Input),
						},
					})
				}
			}
		}
		text := m.Content
		if len(textParts) > 0 {
			text = strings.Join(textParts, "")
		}
		msg.Content = text
		return msg
	}

	content := m.Content
	return AidenMsg{
		Role:    string(m.Role),
		Content: content,
	}
}
