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
	t.Setenv("HOME", dir)
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
	t.Setenv("HOME", dir)
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error when no API key found")
	}
}

func TestLoadAppendsEnvProviderAfterFileProviders(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"providers": [
				{ "name": "aiden", "protocol": "aiden", "baseURL": "https://aiden.example.com" },
				{ "name": "codebase", "protocol": "codebase", "baseURL": "https://codebase.example.com" }
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
	t.Setenv("ANTHROPIC_BASE_URL", "https://env.example.com")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("len(Providers) = %d, want 3", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "aiden" {
		t.Fatalf("Providers[0].Name = %q, want %q", cfg.Providers[0].Name, "aiden")
	}
	if cfg.Providers[1].Name != "codebase" {
		t.Fatalf("Providers[1].Name = %q, want %q", cfg.Providers[1].Name, "codebase")
	}
	if cfg.Providers[2].Name != "env" {
		t.Fatalf("Providers[2].Name = %q, want %q", cfg.Providers[2].Name, "env")
	}
	if cfg.Providers[2].APIKey != "env-override-key" {
		t.Fatalf("Providers[2].APIKey = %q, want %q", cfg.Providers[2].APIKey, "env-override-key")
	}
	if cfg.Providers[2].BaseURL != "https://env.example.com" {
		t.Fatalf("Providers[2].BaseURL = %q, want %q", cfg.Providers[2].BaseURL, "https://env.example.com")
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
					"protocol": "aiden",
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
	if p.Protocol != "aiden" {
		t.Fatalf("Protocol = %q, want %q", p.Protocol, "aiden")
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

func TestLoadParsesToolResultConfig(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"providers": [
				{ "name": "anthropic", "apiKey": "sk-ant-xxx", "baseURL": "https://api.anthropic.com" }
			]
		},
		"tool_result": {
			"inline_max_lines": 300,
			"head_lines": 40,
			"tail_lines": 20
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
	if cfg.ToolResult.InlineMaxLines != 300 {
		t.Fatalf("ToolResult.InlineMaxLines = %d, want 300", cfg.ToolResult.InlineMaxLines)
	}
	if cfg.ToolResult.HeadLines != 40 {
		t.Fatalf("ToolResult.HeadLines = %d, want 40", cfg.ToolResult.HeadLines)
	}
	if cfg.ToolResult.TailLines != 20 {
		t.Fatalf("ToolResult.TailLines = %d, want 20", cfg.ToolResult.TailLines)
	}
}

func TestLoadUsesDefaultToolResultConfig(t *testing.T) {
	dir := t.TempDir()
	settings := `{
		"provider": {
			"providers": [
				{ "name": "anthropic", "apiKey": "sk-ant-xxx", "baseURL": "https://api.anthropic.com" }
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
	if cfg.ToolResult.InlineMaxLines != 200 {
		t.Fatalf("ToolResult.InlineMaxLines = %d, want 200", cfg.ToolResult.InlineMaxLines)
	}
	if cfg.ToolResult.HeadLines != 80 {
		t.Fatalf("ToolResult.HeadLines = %d, want 80", cfg.ToolResult.HeadLines)
	}
	if cfg.ToolResult.TailLines != 80 {
		t.Fatalf("ToolResult.TailLines = %d, want 80", cfg.ToolResult.TailLines)
	}
}

func TestFindSettingsFileFallsBackToGlobal(t *testing.T) {
	homeDir := t.TempDir()
	globalSettings := `{"provider":{"model":"global-model","providers":[{"name":"global","apiKey":"gk","baseURL":"https://global.example.com"}]}}`
	globalPath := filepath.Join(homeDir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(globalSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	path, data := findSettingsFile(projectDir)
	if path != globalPath {
		t.Fatalf("path = %q, want %q", path, globalPath)
	}
	if data == nil {
		t.Fatal("data = nil, want non-nil")
	}
}

func TestFindSettingsFilePrefersProject(t *testing.T) {
	homeDir := t.TempDir()
	globalSettings := `{"provider":{"model":"global-model","providers":[{"name":"global","apiKey":"gk","baseURL":"https://global.example.com"}]}}`
	globalPath := filepath.Join(homeDir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(globalSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	projectSettings := `{"provider":{"model":"project-model","providers":[{"name":"project","apiKey":"pk","baseURL":"https://project.example.com"}]}}`
	projectPath := filepath.Join(projectDir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(projectSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", homeDir)

	path, data := findSettingsFile(projectDir)
	if path != projectPath {
		t.Fatalf("path = %q, want %q", path, projectPath)
	}
	if data == nil {
		t.Fatal("data = nil, want non-nil")
	}
}

func TestFindSettingsFileReturnsNilWhenNeitherExists(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	path, data := findSettingsFile(projectDir)
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}
	if data != nil {
		t.Fatal("data = non-nil, want nil")
	}
}

func TestLoadFallsBackToGlobalSettings(t *testing.T) {
	homeDir := t.TempDir()
	globalSettings := `{
		"provider": {
			"model": "global-model",
			"providers": [
				{ "name": "global-prov", "apiKey": "global-key", "baseURL": "https://global.example.com" }
			]
		},
		"debug": { "enabled": true }
	}`
	globalPath := filepath.Join(homeDir, ".cece", "settings.json")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(globalSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Model != "global-model" {
		t.Fatalf("Model = %q, want %q", cfg.Model, "global-model")
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "global-prov" {
		t.Fatalf("Providers = %+v, want [global-prov]", cfg.Providers)
	}
	if !cfg.Debug {
		t.Fatalf("Debug = %v, want true", cfg.Debug)
	}
}
