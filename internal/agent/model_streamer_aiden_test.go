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

func TestModelStreamerEmitsAidenResponsesOutputTextDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.created\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.output_text.done\",\"content_index\":0,\"output_index\":0,\"text\":\"hello from done\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":3}}}\n\n")
	}))
	defer server.Close()

	client := aiden.NewClient("test-key", "gpt-5.5-paygo", server.URL)
	streamer := agent.NewModelStreamer(client, nil, nil)
	events := make(chan agent.Event, 32)

	_, err := streamer.Stream(context.Background(), agent.ModelStreamRequest{
		Messages:  []agent.Message{{Role: agent.UserRole, Content: "hi"}},
		MaxTokens: 100,
	}, events)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var completed bool
	for len(events) > 0 {
		switch e := (<-events).(type) {
		case agent.AssistantDelta:
			text += e.Text
		case agent.StreamCompleted:
			completed = true
		}
	}

	if text != "hello from done" {
		t.Fatalf("assistant delta text = %q, want output_text.done text", text)
	}
	if !completed {
		t.Fatal("expected StreamCompleted event")
	}
}

// End-to-end test: reasoning + function_call via Responses API.
// Verifies that the thinking block (with provider ID) and tool call
// are both preserved so they can be serialized back in the next request.
func TestModelStreamerPreservesReasoningAndFunctionCallE2E(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.created\"}\n\n")
		// Reasoning item
		fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_test123\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"Let me think\"}],\"encrypted_content\":\"ENC_TEST\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.reasoning_text.delta\",\"output_index\":0,\"delta\":\" about this\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.reasoning_text.done\",\"output_index\":0}\n\n")
		// Function call item
		fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_test1\",\"call_id\":\"call_test1\",\"name\":\"Bash\",\"arguments\":\"\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"delta\":\"{\\\"command\\\":\\\"ls\\\"}\"}\n\n")
		// Output item done events
		fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_test123\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_test1\",\"call_id\":\"call_test1\",\"name\":\"Bash\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
	}))
	defer server.Close()

	client := aiden.NewClient("test-key", "gpt-5.5-paygo", server.URL)
	streamer := agent.NewModelStreamer(client, nil, nil)
	events := make(chan agent.Event, 64)

	resp, err := streamer.Stream(context.Background(), agent.ModelStreamRequest{
		Messages:  []agent.Message{{Role: agent.UserRole, Content: "list files"}},
		MaxTokens: 100,
	}, events)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Drain events and check key events were emitted
	var thinkingStarted, thinkingCompleted, toolCallCompleted, streamCompleted bool
	var thinkingText string
	for len(events) > 0 {
		switch e := (<-events).(type) {
		case agent.ThinkingStarted:
			thinkingStarted = true
		case agent.ThinkingDelta:
			thinkingText += e.Text
		case agent.ThinkingCompleted:
			thinkingCompleted = true
		case agent.ToolCallCompleted:
			toolCallCompleted = true
			if e.Name != "Bash" {
				t.Errorf("tool name = %q, want Bash", e.Name)
			}
		case agent.StreamCompleted:
			streamCompleted = true
		}
	}

	if !thinkingStarted {
		t.Error("expected ThinkingStarted event for reasoning item")
	}
	if !thinkingCompleted {
		t.Error("expected ThinkingCompleted event for reasoning item")
	}
	if !toolCallCompleted {
		t.Error("expected ToolCallCompleted event for function_call item")
	}
	if !streamCompleted {
		t.Error("expected StreamCompleted event")
	}
	_ = resp
}
