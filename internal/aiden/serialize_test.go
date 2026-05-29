package aiden

import (
	"encoding/json"
	"testing"

	"cece/internal/agent"
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
	if result[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", result[0].Content)
	}
}

func TestSerializePlainTextAssistantMessageUsesStringContent(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.AssistantRole, Content: "hi"},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Fatalf("expected role 'assistant', got %q", result[0].Role)
	}
	if result[0].Content != "hi" {
		t.Fatalf("unexpected assistant content: %q", result[0].Content)
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
	if result[0].Content != "You are helpful.\nBe concise." {
		t.Errorf("unexpected system content: %q", result[0].Content)
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
	if got.Content != "I'll run that command." {
		t.Fatalf("unexpected assistant content: %q", got.Content)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("expected tool call id 'call_1', got %q", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected tool call type 'function', got %q", tc.Type)
	}
	if tc.Function.Name != "Bash" {
		t.Errorf("expected function name 'Bash', got %q", tc.Function.Name)
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
	if got.Content != "file1.txt\nfile2.txt" {
		t.Errorf("unexpected content: %q", got.Content)
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
	if result[0].Role != "tool" || result[0].ToolCallID != "call_1" || result[0].Content != "result1" {
		t.Errorf("first tool result: role=%q id=%q content=%q", result[0].Role, result[0].ToolCallID, result[0].Content)
	}
	if result[1].Role != "tool" || result[1].ToolCallID != "call_2" || result[1].Content != "result2" {
		t.Errorf("second tool result: role=%q id=%q content=%q", result[1].Role, result[1].ToolCallID, result[1].Content)
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
	if result[0].Content != "Here is the answer." {
		t.Fatalf("unexpected assistant content: %q", result[0].Content)
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
	if result[0].Content != "Visible answer." {
		t.Fatalf("unexpected assistant content: %q", result[0].Content)
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
	if result[0].Content != "" {
		t.Fatalf("expected assistant content empty, got %q", result[0].Content)
	}
}

func TestSerializeAssistantThinkingAndToolUseKeepsEmptyContent(t *testing.T) {
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
	if result[0].Content != "" {
		t.Fatalf("expected assistant content empty, got %q", result[0].Content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

func TestSerializeAssistantToolOnlyKeepsEmptyContent(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
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
	if result[0].Content != "" {
		t.Fatalf("expected assistant content empty, got %q", result[0].Content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

func TestSerializeJSONRoundTrip(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "hi"},
		{Role: agent.AssistantRole, Content: "hi"},
		{Role: agent.UserRole, Content: "了解下项目结构"},
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
	if len(parsed) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(parsed))
	}
	if parsed[1]["role"] != "assistant" {
		t.Errorf("second message role: %v", parsed[1]["role"])
	}
	content, ok := parsed[1]["content"].(string)
	if !ok {
		t.Fatalf("expected assistant content string, got %T", parsed[1]["content"])
	}
	if content != "hi" {
		t.Fatalf("expected assistant text 'hi', got %v", content)
	}
}
