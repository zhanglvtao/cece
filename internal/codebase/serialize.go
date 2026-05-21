package codebase

import (
	"strings"

	"cece/internal/chat"
	"cece/internal/tool"
)

// CodebaseRequest is the top-level request body for the codebase-api.
type CodebaseRequest struct {
	Model      string            `json:"model"`
	ConfigName string            `json:"config_name"`
	Messages   []CodebaseMessage `json:"messages"`
	MaxTokens  int               `json:"max_tokens,omitempty"`
	Stream     bool              `json:"stream"`
	Tools      []CodebaseTool    `json:"tools,omitempty"`
}

// CodebaseMessage represents a single message in the codebase-api format.
// Unlike OpenAI, content must always be an array of content objects.
type CodebaseMessage struct {
	Role       string             `json:"role"`
	Content    []CodebaseContent  `json:"content"`
	ToolCalls  []CodebaseToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// CodebaseContent is a single content part within a message's content array.
type CodebaseContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// CodebaseToolCall represents a tool call in an assistant message.
// Note: uses "function_call" (not "function" like OpenAI).
type CodebaseToolCall struct {
	Index        int                `json:"index"`
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	FunctionCall *CodebaseFuncCall  `json:"function_call"`
}

// CodebaseFuncCall holds the function name and arguments for a tool call.
type CodebaseFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// CodebaseTool describes a tool in the codebase-api format.
type CodebaseTool struct {
	Type     string          `json:"type"` // "function"
	Function CodebaseToolDef `json:"function"`
}

// CodebaseToolDef holds the function definition within a tool.
type CodebaseToolDef struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Parameters  any       `json:"parameters"`
}

// textContent is a helper to create a single-element content array.
func textContent(text string) []CodebaseContent {
	return []CodebaseContent{{Type: "text", Text: text}}
}

// SerializeMessages converts internal messages + system prompt into codebase-api format.
func SerializeMessages(messages []chat.Message, system chat.SystemPrompt) []CodebaseMessage {
	var result []CodebaseMessage

	if len(system.Blocks) > 0 {
		var sb strings.Builder
		for i, block := range system.Blocks {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(block.Text)
		}
		result = append(result, CodebaseMessage{
			Role:    "system",
			Content: textContent(sb.String()),
		})
	}

	for _, m := range messages {
		result = append(result, serializeMessageExpanded(m)...)
	}

	return result
}

// serializeMessageExpanded returns 1+ CodebaseMessages (multi-tool-result expansion).
func serializeMessageExpanded(m chat.Message) []CodebaseMessage {
	if m.Role == chat.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var msgs []CodebaseMessage
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					msgs = append(msgs, CodebaseMessage{
						Role:       "tool",
						ToolCallID: tr.ToolUseID,
						Content:    textContent(tr.Content),
					})
				}
			}
			return msgs
		}
	}

	return []CodebaseMessage{serializeMessage(m)}
}

func serializeMessage(m chat.Message) CodebaseMessage {
	if len(m.ContentBlocks) > 0 && m.Role == chat.AssistantRole {
		msg := CodebaseMessage{Role: "assistant"}
		var textParts []string
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case chat.ApiTextContentType:
				textParts = append(textParts, cb.Text)
			case chat.ApiToolUseContentType:
				if cb.ToolUse != nil {
					msg.ToolCalls = append(msg.ToolCalls, CodebaseToolCall{
						Index: len(msg.ToolCalls),
						ID:    cb.ToolUse.ID,
						Type:  "function",
						FunctionCall: &CodebaseFuncCall{
							Name:      cb.ToolUse.Name,
							Arguments: string(cb.ToolUse.Input),
						},
					})
				}
			// ApiThinkingContentType: dropped
			}
		}
		if len(textParts) > 0 {
			msg.Content = textContent(strings.Join(textParts, ""))
		}
		return msg
	}

	return CodebaseMessage{
		Role:    string(m.Role),
		Content: textContent(m.Content),
	}
}

// ConvertTools converts internal tool definitions to codebase-api format.
func ConvertTools(tools []tool.Definition) []CodebaseTool {
	result := make([]CodebaseTool, len(tools))
	for i, t := range tools {
		result[i] = CodebaseTool{
			Type: "function",
			Function: CodebaseToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return result
}
