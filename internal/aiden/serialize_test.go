package aiden

import (
	"encoding/json"
	"testing"

	"cece/internal/chat"
)

func TestSerializePlainTextUserMessage(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.UserRole, Content: "hello"},
	}
	result := SerializeMessages(msgs, chat.SystemPrompt{})
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
	msgs := []chat.Message{
		{Role: chat.AssistantRole, Content: "hi"},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Fatalf("expected role 'assistant', got %q", result[0].Role)
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "hi" {
		t.Fatalf("unexpected assistant content: %q", content)
	}
}

func TestSerializeSystemPrompt(t *testing.T) {
	system := chat.SystemPrompt{
		Blocks: []chat.SystemBlock{
			{Text: "You are helpful."},
			{Text: "Be concise."},
		},
	}
	msgs := []chat.Message{
		{Role: chat.UserRole, Content: "hi"},
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
	msgs := []chat.Message{
		{
			Role: chat.AssistantRole,
			ContentBlocks: []chat.ApiContentBlock{
				{Type: chat.ApiTextContentType, Text: "I'll run that command."},
				{
					Type: chat.ApiToolUseContentType,
					ToolUse: &chat.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	got := result[0]
	if got.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", got.Role)
	}
	content, ok := got.Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", got.Content)
	}
	if content != "I'll run that command." {
		t.Fatalf("unexpected assistant content: %q", content)
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
	msgs := []chat.Message{
		{
			Role: chat.UserRole,
			ContentBlocks: []chat.ApiContentBlock{
				{
					Type: chat.ApiToolResultContentType,
					ToolResult: &chat.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "file1.txt\nfile2.txt",
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
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
	msgs := []chat.Message{
		{
			Role: chat.UserRole,
			ContentBlocks: []chat.ApiContentBlock{
				{
					Type: chat.ApiToolResultContentType,
					ToolResult: &chat.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "result1",
					},
				},
				{
					Type: chat.ApiToolResultContentType,
					ToolResult: &chat.ApiToolResultBlock{
						ToolUseID: "call_2",
						Content:   "result2",
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
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
	msgs := []chat.Message{
		{
			Role: chat.AssistantRole,
			ContentBlocks: []chat.ApiContentBlock{
				{Type: chat.ApiThinkingContentType, Text: "let me think..."},
				{Type: chat.ApiTextContentType, Text: "Here is the answer."},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "Here is the answer." {
		t.Fatalf("unexpected assistant content: %q", content)
	}
}

func TestSerializeAssistantThinkingAndLegacyContentKeepsVisibleText(t *testing.T) {
	msgs := []chat.Message{
		{
			Role:    chat.AssistantRole,
			Content: "Visible answer.",
			ContentBlocks: []chat.ApiContentBlock{
				{Type: chat.ApiThinkingContentType, Text: "let me think..."},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "Visible answer." {
		t.Fatalf("unexpected assistant content: %q", content)
	}
}

func TestSerializeAssistantThinkingOnlyUsesEmptyContent(t *testing.T) {
	msgs := []chat.Message{
		{
			Role: chat.AssistantRole,
			ContentBlocks: []chat.ApiContentBlock{
				{Type: chat.ApiThinkingContentType, Text: "let me think..."},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "" {
		t.Fatalf("unexpected assistant content: %q", content)
	}
}

func TestSerializeAssistantThinkingAndToolUseKeepsEmptyContent(t *testing.T) {
	msgs := []chat.Message{
		{
			Role: chat.AssistantRole,
			ContentBlocks: []chat.ApiContentBlock{
				{Type: chat.ApiThinkingContentType, Text: "let me think..."},
				{
					Type: chat.ApiToolUseContentType,
					ToolUse: &chat.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "" {
		t.Fatalf("unexpected assistant content: %q", content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

func TestSerializeAssistantToolOnlyKeepsEmptyContent(t *testing.T) {
	msgs := []chat.Message{
		{
			Role: chat.AssistantRole,
			ContentBlocks: []chat.ApiContentBlock{
				{
					Type: chat.ApiToolUseContentType,
					ToolUse: &chat.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"command":"ls"}`),
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected assistant content as string, got %T", result[0].Content)
	}
	if content != "" {
		t.Fatalf("unexpected assistant content: %q", content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

func TestSerializeJSONRoundTrip(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.UserRole, Content: "hi"},
		{Role: chat.AssistantRole, Content: "hi"},
		{Role: chat.UserRole, Content: "了解下项目结构"},
	}

	result := SerializeMessages(msgs, chat.SystemPrompt{})
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
