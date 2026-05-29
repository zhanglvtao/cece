package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"cece/internal/agent"
)

func TestStreamRequestStripsThinkingBlocks(t *testing.T) {
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "claude-sonnet", server.URL, AuthModeAPIKey)
	ch, err := client.Stream(context.Background(), []agent.Message{{
		Role:    agent.AssistantRole,
		Content: "Visible answer.",
		ContentBlocks: []agent.ApiContentBlock{
			{
				Type: agent.ApiThinkingContentType,
				Thinking: &agent.ApiThinkingBlock{
					Text:      "let me think",
					Signature: "sig_visible",
				},
			},
			{
				Type: agent.ApiRedactedThinkingContentType,
				Thinking: &agent.ApiThinkingBlock{
					Signature: "sig_redacted",
				},
			},
			{Type: agent.ApiTextContentType, Text: "Visible answer."},
			{
				Type: agent.ApiToolUseContentType,
				ToolUse: &agent.ApiToolUseBlock{
					ID:    "toolu_1",
					Name:  "Read",
					Input: json.RawMessage(`{"file_path":"/tmp/x"}`),
				},
			},
		},
	}}, agent.SystemPrompt{}, nil, 256)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	messages := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(messages))
	}
	message := messages[0].(map[string]any)
	blocks := message["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("content blocks len = %d, want 2", len(blocks))
	}

	for _, raw := range blocks {
		block := raw.(map[string]any)
		if block["type"] == "thinking" || block["type"] == "redacted_thinking" {
			t.Fatalf("request payload still contains thinking block: %+v", block)
		}
	}

	text := blocks[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "Visible answer." {
		t.Fatalf("text block = %+v, want visible text", text)
	}
	toolUse := blocks[1].(map[string]any)
	if toolUse["type"] != "tool_use" {
		t.Fatalf("tool block type = %v, want tool_use", toolUse["type"])
	}
	if toolUse["name"] != "Read" || toolUse["id"] != "toolu_1" {
		t.Fatalf("tool block = %+v, want Read/toolu_1", toolUse)
	}
	input := toolUse["input"].(map[string]any)
	if input["file_path"] != "/tmp/x" {
		t.Fatalf("tool input = %+v, want /tmp/x", input)
	}
}
