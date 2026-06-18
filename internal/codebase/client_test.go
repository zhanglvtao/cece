package codebase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/tool"
)

func TestStreamSendsCorrectPayload(t *testing.T) {
	var gotBody CodebaseRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: output\ndata: {\"response\":\"ok\"}\n\n")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "openrouter-2o__dev", "openrouter-2o", server.URL)
	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{Blocks: []agent.SystemBlock{{Text: "You are helpful."}}},
		[]tool.Definition{{Name: "Bash", Description: "Run", InputSchema: map[string]any{"type": "object"}}},
		1024,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if gotBody.Model != "openrouter-2o__dev" {
		t.Errorf("expected model 'openrouter-2o__dev', got %q", gotBody.Model)
	}
	if gotBody.ConfigName != "openrouter-2o" {
		t.Errorf("expected config_name 'openrouter-2o', got %q", gotBody.ConfigName)
	}
	if !gotBody.Stream {
		t.Error("expected stream=true")
	}
	if gotBody.MaxTokens != 1024 {
		t.Errorf("expected max_tokens=1024, got %d", gotBody.MaxTokens)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(gotBody.Messages))
	}
	// Verify content is array format
	if len(gotBody.Messages[0].Content) != 1 || gotBody.Messages[0].Content[0].Type != "text" {
		t.Errorf("expected system content [{type:text}], got %+v", gotBody.Messages[0].Content)
	}
	if len(gotBody.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(gotBody.Tools))
	}
}

func TestStreamSetsBearerAuthAndBusinessID(t *testing.T) {
	var gotAuth, gotBizID, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBizID = r.Header.Get("X-Coco-Business-ID")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("sk-test-token", "model", "config", server.URL)
	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if gotAuth != "Bearer sk-test-token" {
		t.Errorf("expected 'Bearer sk-test-token', got %q", gotAuth)
	}
	if gotBizID != "coco-instance" {
		t.Errorf("expected X-Coco-Business-ID 'coco-instance', got %q", gotBizID)
	}
	if gotUserAgent != ceceUserAgent {
		t.Errorf("expected User-Agent %q, got %q", ceceUserAgent, gotUserAgent)
	}
}

func TestStreamHandlesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"model not found"}`)
	}))
	defer server.Close()

	client := NewClient("key", "bad-model", "config", server.URL)
	_, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got %v", err)
	}
}

func TestStreamWithNoTools(t *testing.T) {
	var gotBody CodebaseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("key", "model", "config", server.URL)
	ch, _ := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	for range ch {
	}

	if gotBody.Tools != nil {
		t.Errorf("expected nil tools when none provided, got %v", gotBody.Tools)
	}
}

func TestStreamStripsThinkingFromPayload(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: output\ndata: {\"response\":\"ok\"}\n\n")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "openrouter-2o__dev", "openrouter-2o", server.URL)
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
					ID:    "call_1",
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
	content := message["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content len = %d, want 1", len(content))
	}
	text := content[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "Visible answer." {
		t.Fatalf("content = %+v, want visible text only", text)
	}
	toolCalls := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(toolCalls))
	}
	toolCall := toolCalls[0].(map[string]any)
	if toolCall["type"] != "function" {
		t.Fatalf("tool_call type = %v, want function", toolCall["type"])
	}
	functionCall := toolCall["function"].(map[string]any)
	if functionCall["name"] != "Read" {
		t.Fatalf("function.name = %v, want Read", functionCall["name"])
	}
	if functionCall["arguments"] != `{"file_path":"/tmp/x"}` {
		t.Fatalf("arguments = %v, want file_path payload", functionCall["arguments"])
	}
}

func TestStreamSecondRequestReplayUsesEmptyContentArrayForToolOnlyAssistant(t *testing.T) {
	var gotBody CodebaseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: output\ndata: {\"response\":\"ok\"}\n\n")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "openrouter-2o__dev", "openrouter-2o", server.URL)
	ch, err := client.Stream(context.Background(), []agent.Message{
		{Role: agent.UserRole, Content: "list files"},
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
		{
			Role: agent.UserRole,
			ContentBlocks: []agent.ApiContentBlock{{
				Type: agent.ApiToolResultContentType,
				ToolResult: &agent.ApiToolResultBlock{
					ToolUseID: "call_1",
					Content:   "file1.txt\nfile2.txt",
				},
			}},
		},
		{Role: agent.UserRole, Content: "continue"},
	}, agent.SystemPrompt{}, nil, 256)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if len(gotBody.Messages) != 4 {
		t.Fatalf("messages len = %d, want 4", len(gotBody.Messages))
	}
	assistant := gotBody.Messages[1]
	if assistant.Role != "assistant" {
		t.Fatalf("assistant role = %q, want assistant", assistant.Role)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls len = %d, want 1", len(assistant.ToolCalls))
	}
	toolCall := assistant.ToolCalls[0]
	if toolCall.Function == nil {
		t.Fatal("assistant tool_call function is nil")
	}
	if toolCall.FunctionCall != nil {
		t.Fatal("assistant tool_call function_call should be nil in outbound history")
	}
	if toolCall.Function.Name != "Bash" {
		t.Fatalf("assistant tool_call function.name = %q, want Bash", toolCall.Function.Name)
	}
	if len(assistant.Content) != 0 {
		t.Fatalf("assistant content = %+v, want empty array", assistant.Content)
	}
	toolResult := gotBody.Messages[2]
	if toolResult.Role != "tool" {
		t.Fatalf("tool result role = %q, want tool", toolResult.Role)
	}
	if toolResult.ToolCallID != "call_1" {
		t.Fatalf("tool result tool_call_id = %q, want call_1", toolResult.ToolCallID)
	}
}

func TestStreamRetriesOn3003(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("content-type", "text/event-stream")
		if attempt == 1 {
			fmt.Fprintf(w, "event: error\ndata: {\"code\":3003,\"message\":\"all models failed\"}\n\n")
			return
		}
		fmt.Fprintf(w, "event: output\ndata: {\"response\":\"ok\"}\n\n")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("key", "model", "config", server.URL)
	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	events, err := collectEvents(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for _, e := range events {
		if e.Delta != "" {
			text += e.Delta
		}
	}
	if text != "ok" {
		t.Errorf("expected text 'ok', got %q", text)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
}

func TestListModelsAndGetModelInfoFromStaticModels(t *testing.T) {
	client := NewClient("key", "openrouter-2o__dev", "openrouter-2o", "https://example.com/chat/completions")
	client.SetModels([]agent.ModelInfo{{
		ID:               "openrouter-2o__dev",
		DisplayName:      "openrouter-2o",
		MaxContextWindow: 936000,
		ConfigName:       "openrouter-2o",
		BaseURL:          "https://example.com",
	}})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ConfigName != "openrouter-2o" {
		t.Fatalf("models = %+v", models)
	}

	info, err := client.GetModelInfo(context.Background())
	if err != nil {
		t.Fatalf("GetModelInfo: %v", err)
	}
	if info.MaxContextWindow != 936000 {
		t.Fatalf("MaxContextWindow = %d", info.MaxContextWindow)
	}
}
