package aiden

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/tool"
)

func TestStreamSendsCorrectPayload(t *testing.T) {
	var gotBody ChatCompletionRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		// Minimal streaming response
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "glm-5.1", server.URL)
	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{Blocks: []agent.SystemBlock{{Text: "You are helpful."}}},
		[]tool.Definition{{Name: "Bash", Description: "Run", InputSchema: map[string]any{"type": "object"}}},
		1024,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Drain the channel
	for range ch {
	}

	if gotBody.Model != "glm-5.1" {
		t.Errorf("expected model 'glm-5.1', got %q", gotBody.Model)
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
	if gotBody.Messages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %q", gotBody.Messages[0].Role)
	}
	if gotBody.Messages[1].Role != "user" {
		t.Errorf("expected second message role 'user', got %q", gotBody.Messages[1].Role)
	}
	if len(gotBody.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(gotBody.Tools))
	}
	if gotBody.Tools[0].Type != "function" {
		t.Errorf("expected tool type 'function', got %q", gotBody.Tools[0].Type)
	}
}

func TestStreamUsesCustomPathPrefix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "model_config", server.URL)
	client.SetPathPrefix("")
	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil,
		1024,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if gotPath != "/chat/completions" {
		t.Fatalf("expected path /chat/completions, got %q", gotPath)
	}
}

func TestListModelsUsesCustomPathPrefix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":"model_config","display_name":"TraeCLI Model","context_length":200000}]}`)
	}))
	defer server.Close()

	client := NewClient("test-key", "model_config", server.URL)
	client.SetPathPrefix("")
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	if gotPath != "/models" {
		t.Fatalf("expected path /models, got %q", gotPath)
	}
	if len(models) != 1 || models[0].ID != "model_config" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestStreamSetsBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("sk-test-token", "glm-5.1", server.URL)
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
}

func TestStreamUsesTokenProvider(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("", "glm-5.1", server.URL)
	client.SetTokenProvider(func(context.Context) (string, error) {
		return "dynamic-token", nil
	})

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

	if gotAuth != "Bearer dynamic-token" {
		t.Errorf("expected 'Bearer dynamic-token', got %q", gotAuth)
	}
}

func TestStreamHandlesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"model not found","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	client := NewClient("key", "bad-model", server.URL)
	_, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got %v", err)
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("expected error to contain 'model not found', got %v", err)
	}
}

func TestStreamWithNoTools(t *testing.T) {
	var gotBody ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("key", "glm-5.1", server.URL)
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

func TestStreamRetriesOn401WithTokenCache(t *testing.T) {
	var callCount int32
	var gotTokens []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		gotTokens = append(gotTokens, auth)
		n := atomic.AddInt32(&callCount, 1)

		if n == 1 {
			// First request: return 401 to simulate expired token
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"token authentication expired","type":"invalid_request_error"}}`)
			return
		}
		// Second request (after retry): success
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	// Use a tokenCache with a helper that returns different tokens on each call
	client := NewClient("", "glm-5.1", server.URL)
	client.SetAuthHelper("echo refreshed-token")

	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err != nil {
		t.Fatalf("Stream after 401 retry: %v", err)
	}
	for range ch {
	}

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", callCount)
	}

	// Both requests should use "Bearer refreshed-token" since Invalidate forces refetch
	if len(gotTokens) != 2 {
		t.Fatalf("expected 2 auth headers, got %d", len(gotTokens))
	}
	if gotTokens[0] != "Bearer refreshed-token" {
		t.Errorf("first request auth = %q, want %q", gotTokens[0], "Bearer refreshed-token")
	}
	if gotTokens[1] != "Bearer refreshed-token" {
		t.Errorf("retry request auth = %q, want %q", gotTokens[1], "Bearer refreshed-token")
	}
}

func TestStreamNoRetryOn401WithoutTokenCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	client := NewClient("static-key", "glm-5.1", server.URL)
	// No SetAuthHelper → no tokenCache → should NOT retry

	_, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 401 without tokenCache")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to contain '401', got %v", err)
	}
}

func TestStreamStripsThinkingFromPayload(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "glm-5.1", server.URL)
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
	content, ok := message["content"].(string)
	if !ok {
		t.Fatalf("content type = %T, want string", message["content"])
	}
	if content != "Visible answer." {
		t.Fatalf("content = %q, want visible text only", content)
	}
	toolCalls := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(toolCalls))
	}
	toolCall := toolCalls[0].(map[string]any)
	if toolCall["type"] != "function" {
		t.Fatalf("tool_call type = %v, want function", toolCall["type"])
	}
	function := toolCall["function"].(map[string]any)
	if function["name"] != "Read" {
		t.Fatalf("function name = %v, want Read", function["name"])
	}
	if function["arguments"] != `{"file_path":"/tmp/x"}` {
		t.Fatalf("arguments = %v, want file_path payload", function["arguments"])
	}
}

func TestStreamResponsesDropsOnlyOrphanFunctionCallOutput(t *testing.T) {
	var gotBody ResponsesAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("expected path /v1/responses, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", "gpt-5.5-paygo", server.URL)
	client.SetUseResponsesAPI(true)
	ch, err := client.Stream(context.Background(), []agent.Message{
		{Role: agent.UserRole, Content: "summary", CompactBoundary: true},
		{Role: agent.ToolRole, ContentBlocks: []agent.ApiContentBlock{{Type: agent.ApiToolResultContentType, ToolResult: &agent.ApiToolResultBlock{ToolUseID: "call_old", Content: "stale replay"}}}},
		{Role: agent.UserRole, Content: "continue"},
	}, agent.SystemPrompt{}, nil, 256)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	var outputs []string
	for _, item := range gotBody.Input {
		if item.Type == "function_call_output" {
			outputs = append(outputs, item.Output)
		}
	}
	if len(outputs) != 0 {
		t.Fatalf("function_call_output count = %d, want 0", len(outputs))
	}
	if len(gotBody.Input) != 2 {
		t.Fatalf("input len = %d, want 2", len(gotBody.Input))
	}
}

func TestStreamNoRetryOnNon401Error(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"bad request","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	client := NewClient("", "glm-5.1", server.URL)
	client.SetAuthHelper("echo token")

	_, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "hi"}},
		agent.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected exactly 1 call for non-401 client error, got %d", callCount)
	}
}
