package aiden

import (
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
)

type ChatCompletionRequest struct {
	Model           string         `json:"model"`
	Messages        []AidenMsg     `json:"messages"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Stream          bool           `json:"stream"`
	StreamOptions   *StreamOptions `json:"stream_options,omitempty"`
	Tools           []AidenTool    `json:"tools,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ResponsesRequest struct {
	Model           string               `json:"model"`
	Instructions    string               `json:"instructions,omitempty"`
	Input           []ResponsesInputItem `json:"input"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Stream          bool                 `json:"stream"`
	Tools           []ResponsesTool      `json:"tools,omitempty"`
	ReasoningEffort string               `json:"reasoning_effort,omitempty"`
}

type ResponsesInputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    any    `json:"output,omitempty"`
	Status    string `json:"status,omitempty"`
}

type AidenMsg struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
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

func SerializeMessages(messages []agent.Message, system agent.SystemPrompt) []AidenMsg {
	var result []AidenMsg

	if s := serializeSystemInstructions(system); s != "" {
		result = append(result, AidenMsg{Role: "system", Content: s})
	}

	for _, m := range messages {
		result = append(result, serializeMessageExpanded(m)...)
	}

	return result
}

func SerializeResponsesInput(messages []agent.Message) []ResponsesInputItem {
	var result []ResponsesInputItem
	for _, m := range messages {
		result = append(result, serializeResponsesMessage(m)...)
	}
	return result
}

func serializeSystemInstructions(system agent.SystemPrompt) string {
	if len(system.Blocks) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, block := range system.Blocks {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(block.Text)
	}
	return sb.String()
}

func serializeMessageExpanded(m agent.Message) []AidenMsg {
	if m.Role == agent.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var msgs []AidenMsg
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					content := tr.Content
					if content == "" {
						content = " "
					}
					msgs = append(msgs, AidenMsg{
						Role:       "tool",
						ToolCallID: tr.ToolUseID,
						Content:    content,
					})
				}
			}
			return msgs
		}
	}

	return []AidenMsg{serializeMessage(m)}
}

func serializeResponsesMessage(m agent.Message) []ResponsesInputItem {
	if m.Role == agent.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var items []ResponsesInputItem
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					item := ResponsesInputItem{
						Type:   "function_call_output",
						CallID: tr.ToolUseID,
						Output: tr.Content,
					}
					if tr.IsError {
						item.Status = "incomplete"
					}
					items = append(items, item)
				}
			}
			return items
		}
	}

	if m.Role == agent.AssistantRole {
		var items []ResponsesInputItem
		text := assistantText(m)
		hasToolCalls := false
		for _, cb := range m.ContentBlocks {
			if cb.Type == agent.ApiToolUseContentType && cb.ToolUse != nil {
				hasToolCalls = true
				break
			}
		}
		if text != "" || hasToolCalls {
			if text == "" {
				text = " "
			}
			items = append(items, ResponsesInputItem{
				Type: "message",
				Role: "assistant",
				Content: []AidenContentPart{
					{Type: "output_text", Text: text},
				},
			})
		} else {
			// Empty assistant message (no text, no tool calls) — include a
			// minimal spacer so the Responses API doesn't see consecutive
			// user messages, which can cause the model to return empty.
			items = append(items, ResponsesInputItem{
				Type:    "message",
				Role:    "assistant",
				Content: []AidenContentPart{{Type: "output_text", Text: " "}},
			})
		}
		for _, cb := range m.ContentBlocks {
			if cb.Type == agent.ApiToolUseContentType && cb.ToolUse != nil {
				items = append(items, ResponsesInputItem{
					Type:      "function_call",
					ID:        responsesFunctionCallID(cb.ToolUse),
					CallID:    cb.ToolUse.ID,
					Name:      cb.ToolUse.Name,
					Arguments: string(cb.ToolUse.Input),
				})
			}
		}
		return items
	}

	text := m.Content
	if text == "" {
		text = m.TextContent()
	}
	return []ResponsesInputItem{{
		Type: "message",
		Role: string(m.Role),
		Content: []AidenContentPart{
			{Type: "input_text", Text: text},
		},
	}}
}

func serializeMessage(m agent.Message) AidenMsg {
	if m.Role == agent.AssistantRole {
		msg := AidenMsg{
			Role: "assistant",
		}
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case agent.ApiToolUseContentType:
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
		msg.Content = assistantText(m)
		// Aiden API requires the content field to be present even for
		// tool-only assistant messages. Use a space as minimal valid content.
		if msg.Content == "" && len(msg.ToolCalls) > 0 {
			msg.Content = " "
		}
		return msg
	}

	content := m.Content
	if content == "" {
		content = m.TextContent()
	}
	if content == "" {
		content = " "
	}
	return AidenMsg{
		Role:    string(m.Role),
		Content: content,
	}
}

func assistantText(m agent.Message) string {
	var textParts []string
	for _, cb := range m.ContentBlocks {
		if cb.Type == agent.ApiTextContentType {
			textParts = append(textParts, cb.Text)
		}
	}
	if len(textParts) > 0 {
		return strings.Join(textParts, "")
	}
	return m.Content
}

func responsesFunctionCallID(toolUse *agent.ApiToolUseBlock) string {
	if toolUse.ProviderID != "" {
		return toolUse.ProviderID
	}
	return "fc_" + toolUse.ID
}
