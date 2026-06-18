package codebase

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
)

type codebaseE2EConfig struct {
	BaseURL    string
	APIKey     string
	AuthHelper string
}

func codebaseE2EConfigFromEnv() (codebaseE2EConfig, bool) {
	cfg := codebaseE2EConfig{
		BaseURL:    strings.TrimSpace(os.Getenv("CECE_CODEBASE_E2E_BASE_URL")),
		APIKey:     strings.TrimSpace(os.Getenv("CECE_CODEBASE_E2E_API_KEY")),
		AuthHelper: strings.TrimSpace(os.Getenv("CECE_CODEBASE_E2E_AUTH_HELPER")),
	}
	if cfg.BaseURL == "" {
		return codebaseE2EConfig{}, false
	}
	if cfg.APIKey == "" && cfg.AuthHelper == "" {
		return codebaseE2EConfig{}, false
	}
	return cfg, true
}

func requireCodebaseE2EConfig(t *testing.T) codebaseE2EConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	cfg, ok := codebaseE2EConfigFromEnv()
	if !ok {
		t.Skip("skipping codebase e2e: set CECE_CODEBASE_E2E_BASE_URL and either CECE_CODEBASE_E2E_API_KEY or CECE_CODEBASE_E2E_AUTH_HELPER")
	}
	return cfg
}

func TestStreamE2E(t *testing.T) {
	cfg := requireCodebaseE2EConfig(t)

	client := NewClient(cfg.APIKey, "openrouter-2o__dev", "openrouter-2o", cfg.BaseURL)
	if cfg.AuthHelper != "" {
		client.SetAuthHelper(cfg.AuthHelper)
	}

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
	cfg := requireCodebaseE2EConfig(t)

	client := NewClient(cfg.APIKey, "DeepSeek-V4-Pro__dev", "DeepSeek-V4-Pro", cfg.BaseURL)
	if cfg.AuthHelper != "" {
		client.SetAuthHelper(cfg.AuthHelper)
	}

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
