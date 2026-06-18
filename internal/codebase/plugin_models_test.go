package codebase

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverCocoPluginModelsFromDir(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `models:
  - name: openrouter-2o
    context_window: 936000
    open_ai:
      model: ignored
    byted_trae:
      base_url: https://codebase-api.byted.org/v2/api/2022-06-01/LLMProxy/TraeV2/chat/completions
      config_name: openrouter-2o
      model: openrouter-2o__dev
  - name: not-codebase
    open_ai:
      model: gpt
`
	if err := os.WriteFile(filepath.Join(pluginDir, "coco.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := DiscoverCocoPluginModelsFromDir(dir)
	if err != nil {
		t.Fatalf("DiscoverCocoPluginModelsFromDir: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models len = %d, want 1", len(models))
	}
	got := models[0]
	if got.ID != "openrouter-2o__dev" {
		t.Fatalf("ID = %q", got.ID)
	}
	if got.DisplayName != "openrouter-2o" {
		t.Fatalf("DisplayName = %q", got.DisplayName)
	}
	if got.ConfigName != "openrouter-2o" {
		t.Fatalf("ConfigName = %q", got.ConfigName)
	}
	if got.BaseURL != "https://codebase-api.byted.org/v2/api/2022-06-01/LLMProxy/TraeV2" {
		t.Fatalf("BaseURL = %q", got.BaseURL)
	}
	if got.MaxContextWindow != 936000 {
		t.Fatalf("MaxContextWindow = %d", got.MaxContextWindow)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	got := normalizeBaseURL("https://x/y/chat/completions/")
	if got != "https://x/y" {
		t.Fatalf("normalizeBaseURL = %q", got)
	}
}
