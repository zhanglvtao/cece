package codebase

import (
	"encoding/json"
	"testing"

	"cece/internal/agent"
	"cece/internal/tool"
)

func TestSerializePlainTextUserMessage(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "hello"},
	}
	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", result[0].Role)
	}
	// codebase-api: content must be array format
	if len(result[0].Content) != 1 || result[0].Content[0].Type != "text" || result[0].Content[0].Text != "hello" {
		t.Errorf("expected content [{type:text, text:hello}], got %+v", result[0].Content)
	}
}

func TestSerializeSystemPrompt(t *testing.T) {
	system := agent.SystemPrompt{
		Blocks: []agent.SystemBlock{
			{Text: "You are helpful."},
			{Text: "Be concise."},
		},
	}
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "hi"},
	}

	result := SerializeMessages(msgs, system)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %q", result[0].Role)
	}
	if len(result[0].Content) != 1 || result[0].Content[0].Text != "You are helpful.\nBe concise." {
		t.Errorf("unexpected system content: %+v", result[0].Content)
	}
}

func TestSerializeAssistantWithTextAndToolUse(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiTextContentType, Text: "I'll run that command."},
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	got := result[0]
	if got.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", got.Role)
	}
	// content is array format
	if len(got.Content) != 1 || got.Content[0].Text != "I'll run that command." {
		t.Errorf("expected content array with text, got %+v", got.Content)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("expected tool call id 'call_1', got %q", tc.ID)
	}
	// History replay uses OpenAI-compatible function key.
	if tc.Function == nil {
		t.Fatal("expected function to be non-nil")
	}
	if tc.FunctionCall != nil {
		t.Fatal("expected function_call to stay nil for serialized history")
	}
	if tc.Function.Name != "Bash" {
		t.Errorf("expected function.name 'Bash', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected arguments: %q", tc.Function.Arguments)
	}
}

func TestSerializeToolResultMessage(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.UserRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "file1.txt\nfile2.txt",
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	got := result[0]
	if got.Role != "tool" {
		t.Errorf("expected role 'tool', got %q", got.Role)
	}
	if got.ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %q", got.ToolCallID)
	}
	// content must be array format
	if len(got.Content) != 1 || got.Content[0].Text != "file1.txt\nfile2.txt" {
		t.Errorf("unexpected content: %+v", got.Content)
	}
}

func TestSerializeMultiToolResultExpansion(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.UserRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "result1",
					},
				},
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_2",
						Content:   "result2",
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (expanded), got %d", len(result))
	}
	if result[0].Role != "tool" || result[0].ToolCallID != "call_1" {
		t.Errorf("first tool result: role=%q id=%q", result[0].Role, result[0].ToolCallID)
	}
	if len(result[0].Content) != 1 || result[0].Content[0].Text != "result1" {
		t.Errorf("first content: %+v", result[0].Content)
	}
	if result[1].Role != "tool" || result[1].ToolCallID != "call_2" {
		t.Errorf("second tool result: role=%q id=%q", result[1].Role, result[1].ToolCallID)
	}
	if len(result[1].Content) != 1 || result[1].Content[0].Text != "result2" {
		t.Errorf("second content: %+v", result[1].Content)
	}
}

func TestSerializeDropsThinkingBlocks(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiThinkingContentType, Text: "let me think..."},
				{Type: agent.ApiTextContentType, Text: "Here is the answer."},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Content) != 1 || result[0].Content[0].Text != "Here is the answer." {
		t.Errorf("expected only text content (thinking dropped), got %+v", result[0].Content)
	}
}

func TestSerializeAssistantThinkingAndLegacyContentKeepsVisibleText(t *testing.T) {
	msgs := []agent.Message{
		{
			Role:    agent.AssistantRole,
			Content: "Visible answer.",
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiThinkingContentType, Text: "let me think..."},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Content) != 1 || result[0].Content[0].Text != "Visible answer." {
		t.Fatalf("expected visible content fallback, got %+v", result[0].Content)
	}
}

func TestSerializeAssistantThinkingOnlyUsesEmptyContent(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiThinkingContentType, Text: "let me think..."},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Content) != 1 || result[0].Content[0].Text != "" {
		t.Fatalf("expected empty content item, got %+v", result[0].Content)
	}
}

func TestSerializeAssistantThinkingAndToolUseUsesEmptyContentArray(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiThinkingContentType, Text: "let me think..."},
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].Content) != 0 {
		t.Fatalf("expected empty content array, got %+v", result[0].Content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

func TestSerializeJSONRoundTrip(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "list files"},
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiTextContentType, Text: "Running ls"},
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "call_abc",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls -la"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parsed))
	}

	// Verify content is array format
	userContent := parsed[0]["content"].([]any)
	if len(userContent) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(userContent))
	}
	contentObj := userContent[0].(map[string]any)
	if contentObj["type"] != "text" {
		t.Errorf("expected content type 'text', got %v", contentObj["type"])
	}

	// Verify serialized history uses OpenAI-compatible function, not function_call.
	toolCalls := parsed[1]["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(toolCalls))
	}
	tc := toolCalls[0].(map[string]any)
	if _, ok := tc["function"]; !ok {
		t.Error("expected 'function' key in tool_call, not found")
	}
	if _, ok := tc["function_call"]; ok {
		t.Error("did not expect 'function_call' key in serialized history tool_call")
	}
}

func TestConvertTools(t *testing.T) {
	tools := []tool.Definition{
		{
			Name:        "Bash",
			Description: "Run a shell command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		},
	}

	result := ConvertTools(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	got := result[0]
	if got.Type != "function" {
		t.Errorf("expected type 'function', got %q", got.Type)
	}
	if got.Function.Name != "Bash" {
		t.Errorf("expected name 'Bash', got %q", got.Function.Name)
	}
	if got.Function.Description != "Run a shell command" {
		t.Errorf("unexpected description: %q", got.Function.Description)
	}
	params, ok := got.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("expected parameters to be map[string]any, got %T", got.Function.Parameters)
	}
	if params["type"] != "object" {
		t.Errorf("expected parameters.type 'object', got %v", params["type"])
	}
}
