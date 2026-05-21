package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileWithProviders(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"model": "kimi-for-coding",
			"maxTokens": 16384,
			"providers": [
				{ "name": "kimi", "apiKey": "sk-kimi-xxx", "baseURL": "https://api.kimi.com/coding" },
				{ "name": "anthropic", "apiKey": "sk-ant-xxx", "baseURL": "https://api.anthropic.com" }
			]
		},
		"debug": { "enabled": true }
	}`
	path := filepath.Join(dir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Model != "kimi-for-coding" {
		t.Fatalf("Model = %q, want %q", cfg.Model, "kimi-for-coding")
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "kimi" {
		t.Fatalf("Providers[0].Name = %q, want %q", cfg.Providers[0].Name, "kimi")
	}
	if cfg.Providers[0].APIKey != "sk-kimi-xxx" {
		t.Fatalf("Providers[0].APIKey = %q, want %q", cfg.Providers[0].APIKey, "sk-kimi-xxx")
	}
	if cfg.Providers[1].BaseURL != "https://api.anthropic.com" {
		t.Fatalf("Providers[1].BaseURL = %q, want %q", cfg.Providers[1].BaseURL, "https://api.anthropic.com")
	}
	if !cfg.Debug {
		t.Fatalf("Debug = %v, want true", cfg.Debug)
	}
}

func TestLoadFallsBackToEnvWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(cfg.Providers))
	}
	if cfg.Providers[0].APIKey != "env-key" {
		t.Fatalf("Providers[0].APIKey = %q, want %q", cfg.Providers[0].APIKey, "env-key")
	}
	if cfg.Providers[0].BaseURL != "https://api.anthropic.com" {
		t.Fatalf("Providers[0].BaseURL = %q, want default %q", cfg.Providers[0].BaseURL, "https://api.anthropic.com")
	}
}

func TestLoadReturnsErrorWhenNoKeyAnywhere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error when no API key found")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"providers": [
				{ "name": "kimi", "apiKey": "file-key", "baseURL": "https://api.kimi.com" }
			]
		}
	}`
	path := filepath.Join(dir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "env-override-key")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// env provider is prepended as providers[0]
	if cfg.Providers[0].APIKey != "env-override-key" {
		t.Fatalf("Providers[0].APIKey = %q, want %q from env override", cfg.Providers[0].APIKey, "env-override-key")
	}
}

func TestLoadParsesStaticModels(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"model": "gpt-4o",
			"providers": [
				{
					"name": "aime",
					"protocol": "openai",
					"baseURL": "https://aime.example.com",
					"authMode": "bearer",
					"authHelper": "echo token",
					"models": [
						{"id": "gpt-4o", "displayName": "GPT-4o", "maxContextWindow": 128000},
						{"id": "deepseek-chat", "displayName": "DeepSeek Chat", "maxContextWindow": 128000}
					]
				}
			]
		}
	}`
	path := filepath.Join(dir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Protocol != "openai" {
		t.Fatalf("Protocol = %q, want %q", p.Protocol, "openai")
	}
	if len(p.Models) != 2 {
		t.Fatalf("len(Models) = %d, want 2", len(p.Models))
	}
	if p.Models[0].ID != "gpt-4o" || p.Models[0].DisplayName != "GPT-4o" || p.Models[0].MaxContextWindow != 128000 {
		t.Fatalf("Models[0] = %+v, want {ID:gpt-4o, DisplayName:GPT-4o, MaxContextWindow:128000}", p.Models[0])
	}
	if p.Models[1].ID != "deepseek-chat" {
		t.Fatalf("Models[1].ID = %q, want %q", p.Models[1].ID, "deepseek-chat")
	}
}

func TestEnvModelOverridesFile(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"model": "kimi-k2.6",
			"providers": [
				{ "name": "kimi", "apiKey": "sk-xxx", "baseURL": "https://api.kimi.com" }
			]
		}
	}`
	path := filepath.Join(dir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-4-7")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Model != "claude-opus-4-7" {
		t.Fatalf("Model = %q, want %q from env override", cfg.Model, "claude-opus-4-7")
	}
}
