package main

import (
	"context"
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/config"
)

func TestBuildListAllModelsUsesModelBaseURLAndDefaultCodebaseHelper(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:     "codebase",
		Protocol: "codebase",
		Models: []config.StaticModel{{
			ID:               "openrouter-2o__dev",
			DisplayName:      "openrouter-2o",
			MaxContextWindow: 936000,
			ConfigName:       "openrouter-2o",
		}},
	}}}

	models, err := buildListAllModelsFn(cfg)(context.Background())
	if err != nil {
		t.Fatalf("buildListAllModelsFn: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models len = %d", len(models))
	}
	if models[0].AuthHelper == "" {
		t.Fatal("expected default codebase auth helper")
	}
	if models[0].ConfigName != "openrouter-2o" {
		t.Fatalf("ConfigName = %q", models[0].ConfigName)
	}
}

func TestBuildAgentModelChoicesIncludesConfiguredModels(t *testing.T) {
	cfg := config.Config{
		Model:      "glm-5.1",
		LightModel: "glm-5.1-mini",
		Providers: []config.ProviderConfig{{
			Name: "static",
			Models: []config.StaticModel{
				{ID: "glm-5.1", ConfigName: "glm-main"},
				{ID: "deepseek-v4-pro", ConfigName: "deepseek"},
			},
		}},
	}

	choices := buildAgentModelChoices(cfg)
	want := []string{"glm-5.1", "glm-5.1-mini", "glm-main", "deepseek-v4-pro", "deepseek"}
	if len(choices) != len(want) {
		t.Fatalf("choices = %#v", choices)
	}
	for i := range want {
		if choices[i] != want[i] {
			t.Fatalf("choices = %#v, want %#v", choices, want)
		}
	}
}

func TestStaticModelsToAgent(t *testing.T) {
	models := staticModelsToAgent([]config.StaticModel{{ID: "m", DisplayName: "M", MaxContextWindow: 123, ConfigName: "c"}})
	if len(models) != 1 {
		t.Fatalf("models len = %d", len(models))
	}
	want := agent.ModelInfo{ID: "m", DisplayName: "M", MaxContextWindow: 123, ConfigName: "c"}
	if models[0] != want {
		t.Fatalf("model = %+v, want %+v", models[0], want)
	}
}
