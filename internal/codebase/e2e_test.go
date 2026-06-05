package codebase

import (
	"context"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

func TestStreamE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	client := NewClient("", "openrouter-2o__dev", "openrouter-2o",
		"https://codebase-api.example.com/v2/api/2022-06-01/LLMProxy/TraeV2")
	client.SetAuthHelper("bytedcli --json auth get-codebase-jwt-token | python3 -c \"import sys,json;print(json.load(sys.stdin)['data']['jwt'])\"")

	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "Say hello in 3 words, nothing else."}},
		agent.SystemPrompt{},
		nil,
		50,
	)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var text string
	var stopReason string
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("Event error: %v", ev.Err)
		}
		if ev.Done {
			break
		}
		if ev.Delta != "" {
			text += ev.Delta
		}
		if ev.EventType == "message_delta" {
			stopReason = ev.StopReason
		}
	}

	if text == "" {
		t.Error("expected non-empty text response")
	}
	t.Logf("Response: %q", text)
	t.Logf("Stop reason: %s", stopReason)
	if stopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", stopReason)
	}
}

func TestStreamE2EWithReasoning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	client := NewClient("", "DeepSeek-V4-Pro__dev", "DeepSeek-V4-Pro",
		"https://codebase-api.example.com/v2/api/2022-06-01/LLMProxy/TraeV2")
	client.SetAuthHelper("bytedcli --json auth get-codebase-jwt-token | python3 -c \"import sys,json;print(json.load(sys.stdin)['data']['jwt'])\"")

	ch, err := client.Stream(context.Background(),
		[]agent.Message{{Role: agent.UserRole, Content: "What is 2+3? Just give the number."}},
		agent.SystemPrompt{},
		nil,
		50,
	)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var text, thinking string
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("Event error: %v", ev.Err)
		}
		if ev.Done {
			break
		}
		if ev.Delta != "" {
			text += ev.Delta
		}
		if ev.ThinkingDelta != "" {
			thinking += ev.ThinkingDelta
		}
	}

	if text == "" {
		t.Error("expected non-empty text response")
	}
	t.Logf("Thinking: %.100s...", thinking)
	t.Logf("Response: %q", text)
}
