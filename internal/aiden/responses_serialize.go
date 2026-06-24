package aiden

import (
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
)

// Responses API request types (OpenAI Responses API protocol).

type ResponsesAPIRequest struct {
	Model           string              `json:"model"`
	Input           []ResponsesItem     `json:"input"`
	Instructions    string              `json:"instructions,omitempty"`
	Tools           []ResponsesTool     `json:"tools,omitempty"`
	Stream          bool                `json:"stream"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Reasoning       *ResponsesReasoning `json:"reasoning,omitempty"`
	Store           bool                `json:"store"`
}

type ResponsesItem struct {
	Type    string             `json:"type"`
	Role    string             `json:"role,omitempty"`
	Content []ResponsesContent `json:"content,omitempty"`

	// function_call fields
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	ID        string `json:"id,omitempty"`

	// function_call_output fields
	Output string `json:"output,omitempty"`
}

type ResponsesContent struct {
	Type string `json:"type"` // "input_text" (user/developer) or "output_text" (assistant)
	Text string `json:"text"`
}

type ResponsesTool struct {
	Type        string         `json:"type"` // "function"
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

type ResponsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// isReasoningModel returns true for models that use the "developer" role
// instead of "system" (o1, o3, o4, gpt-5* but not gpt-5-chat).
// Mirrors Aiden's IV() function.
func isReasoningModel(model string) bool {
	if model == "" {
		return false
	}
	if matched, _ := strings.CutPrefix(model, "o"); matched != "" && len(matched) >= 1 && matched[0] >= '0' && matched[0] <= '9' {
		return true
	}
	if strings.HasPrefix(model, "gpt-5") && !strings.HasPrefix(model, "gpt-5-chat") {
		return true
	}
	return false
}

// SerializeResponsesInput converts agent messages into Responses API input items.
// System messages are NOT included — they should be set as the top-level "instructions" field.
func SerializeResponsesInput(messages []agent.Message, system agent.SystemPrompt, model string) []ResponsesItem {
	var items []ResponsesItem

	for _, m := range messages {
		switch m.Role {
		case agent.UserRole:
			items = append(items, serializeUserMessageItems(m)...)
		case agent.ToolRole:
			items = append(items, serializeToolResultItems(m)...)
		case agent.AssistantRole:
			items = append(items, serializeAssistantMessageItems(m)...)
		}
	}

	return items
}

// SerializeResponsesInstructions extracts system instructions for the "instructions" field.
func SerializeResponsesInstructions(system agent.SystemPrompt) string {
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

// ConvertResponsesTools converts tool definitions to Responses API format.
// Responses API uses internally-tagged function definitions (name at top level).
func ConvertResponsesTools(tools []AidenTool) []ResponsesTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]ResponsesTool, 0, len(tools))
	for _, t := range tools {
		if t.Type == "function" {
			result = append(result, ResponsesTool{
				Type:        "function",
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
	}
	return result
}

// serializeUserMessageItems converts a user message to Responses API items.
// Plain user text → message item with input_text content.
// Tool results → function_call_output items.
func serializeToolResultItems(m agent.Message) []ResponsesItem {
	var items []ResponsesItem
	for _, cb := range m.ContentBlocks {
		if tr, ok := cb.AsToolResult(); ok {
			output := tr.Content
			if output == "" {
				output = " "
			}
			items = append(items, ResponsesItem{
				Type:   "function_call_output",
				CallID: tr.ToolUseID,
				Output: output,
			})
		}
	}
	return items
}

func serializeUserMessageItems(m agent.Message) []ResponsesItem {
	// Plain user message
	content := m.Content
	if content == "" {
		content = m.TextContent()
	}
	if content == "" {
		content = " "
	}
	return []ResponsesItem{
		{
			Type: "message",
			Role: "user",
			Content: []ResponsesContent{
				{Type: "input_text", Text: content},
			},
		},
	}
}

// serializeAssistantMessageItems converts an assistant message to Responses API items.
// Text content → message item with output_text content.
// Tool calls → function_call items.
func serializeAssistantMessageItems(m agent.Message) []ResponsesItem {
	var items []ResponsesItem

	// Build text content for the message item
	text := assistantText(m)

	// Build function_call items from tool_use blocks
	var funcCalls []ResponsesItem
	for _, cb := range m.ContentBlocks {
		if cb.Type == agent.ApiToolUseContentType && cb.ToolUse != nil {
			funcCalls = append(funcCalls, ResponsesItem{
				Type:      "function_call",
				Name:      cb.ToolUse.Name,
				Arguments: string(cb.ToolUse.Input),
				CallID:    cb.ToolUse.ID,
			})
		}
	}

	// Always emit a message item for assistant role.
	// Responses API expects assistant output to have a message item.
	msgText := text
	if msgText == "" {
		msgText = " " // placeholder — API requires non-empty content
	}
	items = append(items, ResponsesItem{
		Type: "message",
		Role: "assistant",
		Content: []ResponsesContent{
			{Type: "output_text", Text: msgText},
		},
	})

	// Append function_call items after the message item
	items = append(items, funcCalls...)

	return items
}
