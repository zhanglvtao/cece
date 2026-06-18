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
