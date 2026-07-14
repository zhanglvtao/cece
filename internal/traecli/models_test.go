package traecli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalModelsReadsCocoPluginModels(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin-a")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	content := []byte(`models:
  - name: GPT-5.5
    context_window: 240000
    description: 'Context window: 240k'
    byted_trae:
      config_name: gpt-5.5
      model: gpt-5.5__dev
  - name: openrouter-3o
    context_window: 936000
    description: 'Context window: 936k'
    byted_trae:
      config_name: openrouter-3o
      model: openrouter-3o__dev
`)
	if err := os.WriteFile(filepath.Join(pluginDir, "coco.yaml"), content, 0o644); err != nil {
		t.Fatalf("write coco.yaml: %v", err)
	}

	models, err := LoadLocalModels(root)
	if err != nil {
		t.Fatalf("LoadLocalModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "GPT-5.5" || models[0].DisplayName != "GPT-5.5" || models[0].MaxContextWindow != 240000 {
		t.Fatalf("models[0] = %+v", models[0])
	}
	if models[1].ID != "openrouter-3o" || models[1].DisplayName != "openrouter-3o" || models[1].MaxContextWindow != 936000 {
		t.Fatalf("models[1] = %+v", models[1])
	}
}

func TestLoadLocalModelsIgnoresAimeTraecliWrapper(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "aime_pc")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	content := []byte(`model:
  name: aime_pc_llm_proxy_plugin
models:
  - name: aime_pc_llm_proxy_plugin
    open_ai:
      api_key: ${PC_LLM_AK}
      base_url: https://aime.bytedance.net/api/agents/v2/llmproxy/app
      model: model_config
`)
	if err := os.WriteFile(filepath.Join(pluginDir, "traecli.yaml"), content, 0o644); err != nil {
		t.Fatalf("write traecli.yaml: %v", err)
	}

	models, err := LoadLocalModels(root)
	if err != nil {
		t.Fatalf("LoadLocalModels: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %+v, want empty", models)
	}
}
