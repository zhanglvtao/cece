package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cece/internal/chat"
	"cece/internal/tool"
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

	client := NewClient("test-key", "gpt-4o", server.URL)
	ch, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{Blocks: []chat.SystemBlock{{Text: "You are helpful."}}},
		[]tool.Definition{{Name: "Bash", Description: "Run", InputSchema: map[string]any{"type": "object"}}},
		1024,
	)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Drain the channel
	for range ch {
	}

	if gotBody.Model != "gpt-4o" {
		t.Errorf("expected model 'gpt-4o', got %q", gotBody.Model)
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

func TestStreamSetsBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient("sk-test-token", "gpt-4o", server.URL)
	ch, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
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

func TestStreamHandlesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"model not found","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	client := NewClient("key", "bad-model", server.URL)
	_, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got %v", err)
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

	client := NewClient("key", "gpt-4o", server.URL)
	ch, _ := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
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
	client := NewClient("", "gpt-4o", server.URL)
	client.SetAuthHelper("echo refreshed-token")

	ch, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
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

	client := NewClient("static-key", "gpt-4o", server.URL)
	// No SetAuthHelper → no tokenCache → should NOT retry

	_, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 401 without tokenCache")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to contain '401', got %v", err)
	}
}

func TestStreamNoRetryOnNon401Error(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"message":"internal error","type":"server_error"}}`)
	}))
	defer server.Close()

	client := NewClient("", "gpt-4o", server.URL)
	client.SetAuthHelper("echo token")

	_, err := client.Stream(context.Background(),
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{},
		nil, 100,
	)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected exactly 1 call for non-401 error, got %d", callCount)
	}
}
