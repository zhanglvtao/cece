package aiden

import (
	"encoding/json"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

func TestSerializeResponsesInputUserMessage(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "hello"},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Type != "message" {
		t.Errorf("expected type 'message', got %q", items[0].Type)
	}
	if items[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", items[0].Role)
	}
	if len(items[0].Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(items[0].Content))
	}
	if items[0].Content[0].Type != "input_text" {
		t.Errorf("expected content type 'input_text', got %q", items[0].Content[0].Type)
	}
	if items[0].Content[0].Text != "hello" {
		t.Errorf("expected text 'hello', got %q", items[0].Content[0].Text)
	}
}

func TestSerializeResponsesInputAssistantMessage(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.AssistantRole, Content: "hi there"},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Type != "message" {
		t.Errorf("expected type 'message', got %q", items[0].Type)
	}
	if items[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", items[0].Role)
	}
	if len(items[0].Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(items[0].Content))
	}
	// Critical: assistant text must be output_text, not input_text
	if items[0].Content[0].Type != "output_text" {
		t.Errorf("expected content type 'output_text', got %q", items[0].Content[0].Type)
	}
	if items[0].Content[0].Text != "hi there" {
		t.Errorf("expected text 'hi there', got %q", items[0].Content[0].Text)
	}
}

func TestSerializeResponsesInputAssistantWithToolCall(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{Type: agent.ApiTextContentType, Text: "let me check"},
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Bash",
						Input: json.RawMessage(`{"cmd":"ls"}`),
					},
				},
			},
		},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	// Should produce: message item (output_text) + function_call item
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// First: message with output_text
	if items[0].Type != "message" || items[0].Role != "assistant" {
		t.Errorf("expected message/assistant, got %q/%q", items[0].Type, items[0].Role)
	}
	if len(items[0].Content) != 1 || items[0].Content[0].Type != "output_text" {
		t.Errorf("expected output_text content, got %+v", items[0].Content)
	}
	if items[0].Content[0].Text != "let me check" {
		t.Errorf("expected text 'let me check', got %q", items[0].Content[0].Text)
	}

	// Second: function_call
	if items[1].Type != "function_call" {
		t.Errorf("expected type 'function_call', got %q", items[1].Type)
	}
	if items[1].Name != "Bash" {
		t.Errorf("expected name 'Bash', got %q", items[1].Name)
	}
	if items[1].CallID != "call_1" {
		t.Errorf("expected call_id 'call_1', got %q", items[1].CallID)
	}
	if items[1].Arguments != `{"cmd":"ls"}` {
		t.Errorf("expected arguments '{\"cmd\":\"ls\"}', got %q", items[1].Arguments)
	}
}

func TestSerializeResponsesInputToolOnlyAssistant(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.AssistantRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolUseContentType,
					ToolUse: &agent.ApiToolUseBlock{
						ID:    "call_1",
						Name:  "Read",
						Input: json.RawMessage(`{"path":"/tmp"}`),
					},
				},
			},
		},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	// Should produce: message item (with placeholder output_text) + function_call item
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// First: message with placeholder content
	if items[0].Type != "message" || items[0].Role != "assistant" {
		t.Errorf("expected message/assistant, got %q/%q", items[0].Type, items[0].Role)
	}
	if len(items[0].Content) != 1 || items[0].Content[0].Type != "output_text" {
		t.Errorf("expected output_text content, got %+v", items[0].Content)
	}
	if items[0].Content[0].Text != " " {
		t.Errorf("expected placeholder ' ', got %q", items[0].Content[0].Text)
	}
	// Second: function_call
	if items[1].Type != "function_call" || items[1].Name != "Read" {
		t.Errorf("expected function_call/Read, got %q/%q", items[1].Type, items[1].Name)
	}
}

func TestSerializeResponsesInputToolResult(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.ToolRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "file content here",
					},
				},
			},
		},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Type != "function_call_output" {
		t.Errorf("expected type 'function_call_output', got %q", items[0].Type)
	}
	if items[0].CallID != "call_1" {
		t.Errorf("expected call_id 'call_1', got %q", items[0].CallID)
	}
	if items[0].Output != "file content here" {
		t.Errorf("expected output 'file content here', got %q", items[0].Output)
	}
}

func TestSerializeResponsesInputMultipleToolResults(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.ToolRole,
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
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].CallID != "call_1" || items[0].Output != "result1" {
		t.Errorf("item 0: expected call_1/result1, got %q/%q", items[0].CallID, items[0].Output)
	}
	if items[1].CallID != "call_2" || items[1].Output != "result2" {
		t.Errorf("item 1: expected call_2/result2, got %q/%q", items[1].CallID, items[1].Output)
	}
}

func TestSerializeResponsesInstructions(t *testing.T) {
	system := agent.SystemPrompt{
		Blocks: []agent.SystemBlock{
			{Text: "You are a helpful assistant."},
			{Text: "Be concise."},
		},
	}
	result := SerializeResponsesInstructions(system)
	expected := "You are a helpful assistant.\nBe concise."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSerializeResponsesInstructionsEmpty(t *testing.T) {
	result := SerializeResponsesInstructions(agent.SystemPrompt{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSerializeResponsesInputFullConversation(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.UserRole, Content: "what is 1+1?"},
		{Role: agent.AssistantRole, Content: "2"},
		{Role: agent.UserRole, Content: "thanks"},
	}
	items := SerializeResponsesInput(msgs, agent.SystemPrompt{}, "gpt-5")

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// user → input_text
	if items[0].Content[0].Type != "input_text" {
		t.Errorf("item 0: expected input_text, got %q", items[0].Content[0].Type)
	}
	// assistant → output_text (the critical bug fix)
	if items[1].Content[0].Type != "output_text" {
		t.Errorf("item 1: expected output_text, got %q", items[1].Content[0].Type)
	}
	// user → input_text
	if items[2].Content[0].Type != "input_text" {
		t.Errorf("item 2: expected input_text, got %q", items[2].Content[0].Type)
	}
}

func TestConvertResponsesTools(t *testing.T) {
	tools := []AidenTool{
		{
			Type: "function",
			Function: AidenToolDef{
				Name:        "Bash",
				Description: "Run a command",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	}
	result := ConvertResponsesTools(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	if result[0].Type != "function" {
		t.Errorf("expected type 'function', got %q", result[0].Type)
	}
	if result[0].Name != "Bash" {
		t.Errorf("expected name 'Bash', got %q", result[0].Name)
	}
	if result[0].Description != "Run a command" {
		t.Errorf("expected description, got %q", result[0].Description)
	}
}

func TestIsReasoningModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"o1", true},
		{"o3-mini", true},
		{"o4-mini", true},
		{"gpt-5", true},
		{"gpt-5.4-pro", true},
		{"gpt-5-chat-latest", false},
		{"gpt-4o", false},
		{"deepseek-chat", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isReasoningModel(tt.model)
		if got != tt.want {
			t.Errorf("isReasoningModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}
