package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/aiden"
)

func TestModelStreamerCompletesAidenChatCompletionAfterTerminalFinishReasonWithoutDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprintf(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1}}\n\n")
	}))
	defer server.Close()

	client := aiden.NewClient("test-key", "glm-5.1", server.URL)
	streamer := agent.NewModelStreamer(client, nil, nil)
	events := make(chan agent.Event, 32)

	_, err := streamer.Stream(context.Background(), agent.ModelStreamRequest{
		Messages:  []agent.Message{{Role: agent.UserRole, Content: "hi"}},
		MaxTokens: 100,
	}, events)
	if err != nil {
		if strings.Contains(err.Error(), "stream ended without message_stop") {
			t.Fatalf("unexpected missing Done error: %v", err)
		}
		t.Fatalf("Stream: %v", err)
	}

	var completed bool
	for len(events) > 0 {
		if _, ok := (<-events).(agent.StreamCompleted); ok {
			completed = true
		}
	}
	if !completed {
		t.Fatal("expected StreamCompleted event")
	}
}
