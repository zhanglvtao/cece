package aiden

import (
	"encoding/json"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
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
	// Content is set to " " (space) to meet Aiden API requirement that content field must exist
	if result[0].Content != " " {
		t.Fatalf("expected assistant content ' ' (space placeholder for API), got %q", result[0].Content)
	}
	// Verify JSON contains content field even when there's no visible text
	data, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["content"]; !ok {
		t.Fatalf("content field missing from JSON: %s", string(data))
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
	if result[0].Content != " " {
		t.Fatalf("expected assistant content ' ' (space placeholder), got %q", result[0].Content)
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
	if result[0].Content != " " {
		t.Fatalf("expected assistant content ' ' (space placeholder), got %q", result[0].Content)
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
}

// TestSerializeEmptyUserContentPreservesField reproduces the bug where a user
// message with empty Content and no text ContentBlocks would serialize without
// a "content" field (due to omitempty), causing aiden API to return 400:
// "The content field is a required field."
func TestSerializeEmptyUserContentPreservesField(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.UserRole,
			// Content is empty, ContentBlocks has no text type — TextContent() returns ""
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "", // empty tool result
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	// The key check: JSON must contain "content" field even when empty
	data, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := parsed["content"]; !ok {
		t.Fatalf("BUG REPRODUCED: 'content' field missing from JSON: %s", string(data))
	}
	content, ok := parsed["content"].(string)
	if !ok {
		t.Fatalf("expected content string, got %T", parsed["content"])
	}
	if content == "" {
		t.Fatalf("BUG REPRODUCED: content is empty string, will be omitted by omitempty: %s", string(data))
	}
	t.Logf("content preserved as: %q (JSON: %s)", content, string(data))
}

// TestSerializeEmptyToolResultContentPreservesField reproduces the bug where
// a tool message with empty Content would serialize without a "content" field.
func TestSerializeEmptyToolResultContentPreservesField(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.UserRole,
			ContentBlocks: []agent.ApiContentBlock{
				{
					Type: agent.ApiToolResultContentType,
					ToolResult: &agent.ApiToolResultBlock{
						ToolUseID: "call_1",
						Content:   "", // empty — triggers the bug
					},
				},
			},
		},
	}

	result := SerializeMessages(msgs, agent.SystemPrompt{})
	// Tool result messages get expanded to role="tool"
	if len(result) != 1 {
		t.Fatalf("expected 1 tool message, got %d", len(result))
	}
	if result[0].Role != "tool" {
		t.Fatalf("expected role 'tool', got %q", result[0].Role)
	}

	data, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := parsed["content"]; !ok {
		t.Fatalf("BUG REPRODUCED: 'content' field missing from tool message JSON: %s", string(data))
	}
	content, _ := parsed["content"].(string)
	if content == "" {
		t.Fatalf("BUG REPRODUCED: tool content is empty string: %s", string(data))
	}
	t.Logf("tool message content preserved as: %q (JSON: %s)", content, string(data))
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

func TestMergeConsecutiveAssistant(t *testing.T) {
	tests := []struct {
		name string
		in   []agent.Message
		want []agent.Message
	}{
		{
			name: "single message",
			in: []agent.Message{
				{Role: agent.UserRole, Content: "hi"},
			},
			want: []agent.Message{
				{Role: agent.UserRole, Content: "hi"},
			},
		},
		{
			name: "no consecutive assistant",
			in: []agent.Message{
				{Role: agent.UserRole, Content: "hello"},
				{Role: agent.AssistantRole, Content: "hi"},
				{Role: agent.UserRole, Content: "bye"},
			},
			want: []agent.Message{
				{Role: agent.UserRole, Content: "hello"},
				{Role: agent.AssistantRole, Content: "hi"},
				{Role: agent.UserRole, Content: "bye"},
			},
		},
		{
			name: "merge two consecutive assistant messages",
			in: []agent.Message{
				{Role: agent.UserRole, Content: "hello"},
				{Role: agent.AssistantRole, Content: "[Empty response — retrying]"},
				{Role: agent.AssistantRole, Content: " ", ContentBlocks: []agent.ApiContentBlock{
					{Type: agent.ApiToolUseContentType, ToolUse: &agent.ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{}`)}},
				}},
				{Role: agent.UserRole, Content: "next"},
			},
			want: []agent.Message{
				{Role: agent.UserRole, Content: "hello"},
				{Role: agent.AssistantRole, Content: "[Empty response — retrying]\n ", ContentBlocks: []agent.ApiContentBlock{
					{Type: agent.ApiToolUseContentType, ToolUse: &agent.ApiToolUseBlock{ID: "call_1", Name: "Bash", Input: json.RawMessage(`{}`)}},
				}},
				{Role: agent.UserRole, Content: "next"},
			},
		},
		{
			name: "merge three consecutive assistant messages",
			in: []agent.Message{
				{Role: agent.UserRole, Content: "test"},
				{Role: agent.AssistantRole, Content: "retry"},
				{Role: agent.AssistantRole, Content: "retry2"},
				{Role: agent.AssistantRole, Content: "ok"},
			},
			want: []agent.Message{
				{Role: agent.UserRole, Content: "test"},
				{Role: agent.AssistantRole, Content: "retry\nretry2\nok"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConsecutiveAssistant(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len: got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Role != tt.want[i].Role {
					t.Errorf("[%d] role: got %q, want %q", i, got[i].Role, tt.want[i].Role)
				}
				if got[i].Content != tt.want[i].Content {
					t.Errorf("[%d] content: got %q, want %q", i, got[i].Content, tt.want[i].Content)
				}
				if len(got[i].ContentBlocks) != len(tt.want[i].ContentBlocks) {
					t.Errorf("[%d] content_blocks: got %d, want %d", i, len(got[i].ContentBlocks), len(tt.want[i].ContentBlocks))
				}
			}
		})
	}
}
