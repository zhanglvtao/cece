package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cece/internal/chat"
)

func TestStreamSendsSystemPrompt(t *testing.T) {
	var gotPayload map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		gotPayload = raw

		w.Header().Set("content-type", "text/event-stream")
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	client := NewClient("test-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)
	system := chat.SystemPrompt{
		Blocks: []chat.SystemBlock{
			{Text: "You are helpful.", CacheControl: map[string]string{"type": "ephemeral"}},
			{Text: "Use Chinese."},
			{Text: "cwd: /repo", CacheControl: map[string]string{"type": "ephemeral"}},
		},
	}

	_, err := client.Stream(context.Background(), nil, system, nil, 1024)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	// Verify system field was sent
	sysRaw, ok := gotPayload["system"]
	if !ok {
		t.Fatal("payload missing 'system' field")
	}

	var systemBlocks []map[string]any
	if err := json.Unmarshal(sysRaw, &systemBlocks); err != nil {
		t.Fatalf("failed to unmarshal system: %v", err)
	}

	if len(systemBlocks) != 3 {
		t.Fatalf("len(system) = %d, want 3", len(systemBlocks))
	}

	// First block: should have cache_control
	if systemBlocks[0]["text"] != "You are helpful." {
		t.Fatalf("system[0].text = %v, want %q", systemBlocks[0]["text"], "You are helpful.")
	}
	cc0, ok := systemBlocks[0]["cache_control"].(map[string]any)
	if !ok || cc0["type"] != "ephemeral" {
		t.Fatalf("system[0].cache_control = %v, want ephemeral", systemBlocks[0]["cache_control"])
	}

	// Second block: should NOT have cache_control
	if systemBlocks[1]["text"] != "Use Chinese." {
		t.Fatalf("system[1].text = %v, want %q", systemBlocks[1]["text"], "Use Chinese.")
	}
	if _, has := systemBlocks[1]["cache_control"]; has {
		t.Fatal("system[1] should not have cache_control")
	}

	// Third block: should have cache_control
	cc2, ok := systemBlocks[2]["cache_control"].(map[string]any)
	if !ok || cc2["type"] != "ephemeral" {
		t.Fatalf("system[2].cache_control = %v, want ephemeral", systemBlocks[2]["cache_control"])
	}
}

func TestStreamOmitsSystemWhenEmpty(t *testing.T) {
	var gotPayload map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		gotPayload = raw

		w.Header().Set("content-type", "text/event-stream")
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0}}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	client := NewClient("test-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)

	_, err := client.Stream(context.Background(), nil, chat.SystemPrompt{}, nil, 1024)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	// Empty system should produce no "system" key (omitempty)
	if _, has := gotPayload["system"]; has {
		t.Fatal("empty SystemPrompt should omit 'system' field in payload")
	}
}
