package recording

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/aiden"
)

const recordLLMExpectedText = "cece-record-ok"

type recordLLMConfig struct {
	model        string
	baseURL      string
	apiKey       string
	authHelper   string
	cassettePath string
}

func TestRealLLMRecord_AidenGLM51(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM record test in short mode")
	}
	if strings.TrimSpace(os.Getenv("CECE_RECORD_LLM")) != "1" {
		t.Skip("skipping real LLM record test: set CECE_RECORD_LLM=1")
	}

	cfg := recordLLMConfigFromEnv(t)
	client := aiden.NewClient(cfg.apiKey, cfg.model, cfg.baseURL)
	if cfg.authHelper != "" {
		client.SetAuthHelper(cfg.authHelper)
	}
	recorder := NewRecordingClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	text, err := recordLLMStreamText(ctx, recorder)
	if err != nil {
		t.Fatalf("stream real LLM: %v", err)
	}
	if !strings.Contains(strings.ToLower(text), recordLLMExpectedText) {
		t.Fatalf("real LLM text = %q, want it to contain %q", text, recordLLMExpectedText)
	}

	cassette := recorder.Cassette()
	if len(cassette.Turns) != 1 {
		t.Fatalf("recorded turns = %d, want 1", len(cassette.Turns))
	}
	if len(cassette.Turns[0].Events) == 0 {
		t.Fatal("recorded cassette has no events")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.cassettePath), 0o755); err != nil {
		t.Fatalf("create cassette dir: %v", err)
	}
	if err := Save(cfg.cassettePath, cassette); err != nil {
		t.Fatalf("save cassette: %v", err)
	}

	loaded, err := Load(cfg.cassettePath)
	if err != nil {
		t.Fatalf("load cassette: %v", err)
	}
	replayText, err := recordLLMStreamText(context.Background(), NewReplayClient(loaded))
	if err != nil {
		t.Fatalf("replay cassette: %v", err)
	}
	if replayText != text {
		t.Fatalf("replay text = %q, want recorded text %q", replayText, text)
	}

	t.Logf("recorded %s response to %s", cfg.model, cfg.cassettePath)
}

func recordLLMConfigFromEnv(t *testing.T) recordLLMConfig {
	t.Helper()

	model := strings.TrimSpace(os.Getenv("CECE_RECORD_LLM_MODEL"))
	if model == "" {
		model = "glm-5.1"
	}
	baseURL := strings.TrimSpace(os.Getenv("CECE_RECORD_LLM_BASE_URL"))
	if baseURL == "" {
		t.Fatal("CECE_RECORD_LLM_BASE_URL is required for aiden record test")
	}
	apiKey := strings.TrimSpace(os.Getenv("CECE_RECORD_LLM_API_KEY"))
	authHelper := strings.TrimSpace(os.Getenv("CECE_RECORD_LLM_AUTH_HELPER"))
	if apiKey == "" && authHelper == "" {
		t.Fatal("CECE_RECORD_LLM_API_KEY or CECE_RECORD_LLM_AUTH_HELPER is required")
	}
	cassettePath := strings.TrimSpace(os.Getenv("CECE_RECORD_LLM_CASSETTE"))
	if cassettePath == "" {
		cassettePath = filepath.Join("..", "testdata", "aiden-glm-5.1-basic.cassette.json")
	}

	return recordLLMConfig{
		model:        model,
		baseURL:      baseURL,
		apiKey:       apiKey,
		authHelper:   authHelper,
		cassettePath: cassettePath,
	}
}

func recordLLMStreamText(ctx context.Context, client agent.ModelClient) (string, error) {
	ch, err := client.Stream(ctx,
		[]agent.Message{{Role: agent.UserRole, Content: "Reply with exactly: " + recordLLMExpectedText}},
		agent.SystemPrompt{Blocks: []agent.SystemBlock{{Text: "Return exactly what the user asks for, with no extra text."}}},
		nil,
		32,
	)
	if err != nil {
		return "", err
	}

	var text strings.Builder
	for ev := range ch {
		if ev.Err != nil {
			return "", ev.Err
		}
		if ev.Delta != "" {
			text.WriteString(ev.Delta)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("empty response text")
	}
	return text.String(), nil
}
