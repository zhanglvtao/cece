package aiden

import (
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
)

type ChatCompletionRequest struct {
	Model           string      `json:"model"`
	Messages        []AidenMsg  `json:"messages"`
	MaxTokens       int         `json:"max_tokens,omitempty"`
	Stream          bool        `json:"stream"`
	Tools           []AidenTool `json:"tools,omitempty"`
	ReasoningEffort string      `json:"reasoning_effort,omitempty"`
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

	merged := mergeConsecutiveAssistant(messages)
	for _, m := range merged {
		result = append(result, serializeMessageExpanded(m)...)
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
		// Cover all empty content cases: pure thinking, no tool calls, etc.
		if msg.Content == "" {
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

// mergeConsecutiveAssistant merges consecutive assistant-role messages into one.
// This prevents aiden proxy's Responses API conversion from breaking on
// consecutive assistant messages (e.g. "[Empty response — retrying]" followed
// by a real assistant message with tool_calls).
func mergeConsecutiveAssistant(messages []agent.Message) []agent.Message {
	if len(messages) < 2 {
		return messages
	}
	var result []agent.Message
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		if m.Role != agent.AssistantRole || len(result) == 0 || result[len(result)-1].Role != agent.AssistantRole {
			result = append(result, m)
			continue
		}
		// Merge into previous assistant message.
		prev := &result[len(result)-1]
		if m.Content != "" {
			if prev.Content != "" {
				prev.Content += "\n" + m.Content
			} else {
				prev.Content = m.Content
			}
		}
		prev.ContentBlocks = append(prev.ContentBlocks, m.ContentBlocks...)
	}
	return result
}
