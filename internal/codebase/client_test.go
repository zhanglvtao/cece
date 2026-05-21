package codebase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cece/internal/chat"
	"cece/internal/tool"
)

func TestStreamSendsCorrectPayload(t *testing.T) {
	var gotBody CodebaseRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/context/chat/completions" {
			t.Errorf("expected path /context/chat/completions, got %s", r.URL.Path)
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
		[]chat.Message{{Role: chat.UserRole, Content: "hi"}},
		chat.SystemPrompt{Blocks: []chat.SystemBlock{{Text: "You are helpful."}}},
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
	var gotAuth, gotBizID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBizID = r.Header.Get("X-Coco-Business-ID")
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "event: done\ndata: {\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("sk-test-token", "model", "config", server.URL)
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
	if gotBizID != "coco-instance" {
		t.Errorf("expected X-Coco-Business-ID 'coco-instance', got %q", gotBizID)
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
