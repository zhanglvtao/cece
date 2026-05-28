package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultModel = "claude-sonnet-4-6"
const settingsRelPath = ".cece/settings.json"
const (
	defaultToolResultInlineMaxLines = 200
	defaultToolResultHeadLines      = 80
	defaultToolResultTailLines      = 80
)

// ProviderConfig describes a single API provider's credentials.
type ProviderConfig struct {
	Name       string        `json:"name"`
	Protocol   string        `json:"protocol"`   // "anthropic" (default), "aiden", or "codebase"
	APIKey     string        `json:"apiKey"`
	BaseURL    string        `json:"baseURL"`
	AuthMode   string        `json:"authMode"`   // "apikey" (default) or "bearer"
	AuthHelper string        `json:"authHelper"` // shell command to fetch dynamic token
	Models     []StaticModel `json:"models"`     // static model list (for providers without /v1/models)
}

// StaticModel declares a model available from a provider.
type StaticModel struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	MaxContextWindow int    `json:"maxContextWindow"`
	ConfigName       string `json:"configName"` // codebase-api needs this field
}

type ToolResultConfig struct {
	InlineMaxLines int
	HeadLines      int
	TailLines      int
}

// MCPType specifies the transport type for an MCP server connection.
type MCPType string

const (
	MCPStdio          MCPType = "stdio"
	MCPsse            MCPType = "sse"
	MCPStreamableHTTP MCPType = "streamable-http"
)

// MCPConfig describes a single MCP server connection.
type MCPConfig struct {
	Type     MCPType          `json:"type"`              // "stdio", "sse", or "streamable-http"
	URL      string           `json:"url,omitempty"`     // for sse / streamable-http
	Command  string           `json:"command,omitempty"` // for stdio
	Args     []string         `json:"args,omitempty"`    // for stdio
	Env      map[string]string `json:"env,omitempty"`    // for stdio
	Headers  map[string]string `json:"headers,omitempty"` // for sse / streamable-http
	Disabled bool             `json:"disabled,omitempty"`
	Timeout  int              `json:"timeout,omitempty"` // seconds, default 15
}

type MCPs map[string]MCPConfig

type Config struct {
	Model               string
	Debug               bool
	Yolo                bool
	MaxTokens           int
	ModelContextMapping map[string]int // model ID -> max context window
	Providers           []ProviderConfig
	ToolResult          ToolResultConfig
	MCP                 MCPs
}

type settingsFile struct {
	Provider struct {
		Model               string           `json:"model"`
		MaxTokens           int              `json:"maxTokens"`
		ModelContextMapping map[string]int   `json:"modelContextMapping"`
		Providers           []ProviderConfig `json:"providers"`
	} `json:"provider"`
	Debug struct {
		Enabled bool `json:"enabled"`
	} `json:"debug"`
	Yolo struct {
		Enabled bool `json:"enabled"`
	} `json:"yolo"`
	ToolResult struct {
		InlineMaxLines int `json:"inline_max_lines"`
		HeadLines      int `json:"head_lines"`
		TailLines      int `json:"tail_lines"`
	} `json:"tool_result"`
	MCP MCPs `json:"mcp"`
}

func Load(projectDir string) (Config, error) {
	cfg := Config{
		ToolResult: defaultToolResultConfig(),
	}

	path, data := findSettingsFile(projectDir)
	if data != nil {
		var sf settingsFile
		if err := json.Unmarshal(data, &sf); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg.Model = strings.TrimSpace(sf.Provider.Model)
		cfg.MaxTokens = sf.Provider.MaxTokens
		cfg.ModelContextMapping = sf.Provider.ModelContextMapping
		cfg.Providers = sf.Provider.Providers
		cfg.Debug = sf.Debug.Enabled
		cfg.Yolo = sf.Yolo.Enabled
		cfg.ToolResult = ToolResultConfig{
			InlineMaxLines: sf.ToolResult.InlineMaxLines,
			HeadLines:      sf.ToolResult.HeadLines,
			TailLines:      sf.ToolResult.TailLines,
		}
		cfg.MCP = sf.MCP
	}
	cfg.ToolResult = normalizeToolResultConfig(cfg.ToolResult)

	if v := os.Getenv("ZLAUDE_YOLO"); v == "1" || v == "true" {
		cfg.Yolo = true
	}

	// Environment variable fallback: add an "env" provider when set.
	// Keep file-defined provider order so startup routing remains stable.
	if envKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); envKey != "" {
		envBaseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))
		if envBaseURL == "" {
			envBaseURL = "https://api.anthropic.com"
		}
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name:    "env",
			APIKey:  envKey,
			BaseURL: envBaseURL,
		})
	}

	if len(cfg.Providers) == 0 {
		return Config{}, errors.New("no providers configured: add providers to .cece/settings.json or ~/.cece/settings.json, or set ANTHROPIC_API_KEY")
	}

	if envModel := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); envModel != "" {
		cfg.Model = envModel
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}

	if v := os.Getenv("ANTHROPIC_MAX_TOKENS"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.MaxTokens)
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 16384
	}

	return cfg, nil
}

// findSettingsFile returns the path and contents of the settings file.
// It first tries the project-level path, then falls back to ~/.cece/settings.json.
func findSettingsFile(projectDir string) (string, []byte) {
	projectPath := filepath.Join(projectDir, settingsRelPath)
	if data, err := os.ReadFile(projectPath); err == nil {
		return projectPath, data
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil
	}
	globalPath := filepath.Join(home, settingsRelPath)
	if data, err := os.ReadFile(globalPath); err == nil {
		return globalPath, data
	}
	return "", nil
}

func defaultToolResultConfig() ToolResultConfig {
	return ToolResultConfig{
		InlineMaxLines: defaultToolResultInlineMaxLines,
		HeadLines:      defaultToolResultHeadLines,
		TailLines:      defaultToolResultTailLines,
	}
}

func normalizeToolResultConfig(cfg ToolResultConfig) ToolResultConfig {
	defaults := defaultToolResultConfig()
	if cfg.InlineMaxLines <= 0 {
		cfg.InlineMaxLines = defaults.InlineMaxLines
	}
	if cfg.HeadLines <= 0 {
		cfg.HeadLines = defaults.HeadLines
	}
	if cfg.TailLines <= 0 {
		cfg.TailLines = defaults.TailLines
	}
	return cfg
}

const defaultContextWindow = 200000

// ContextWindowFor returns the context window for the given model.
// Priority: mapping table lookup, then default (200K).
func (c Config) ContextWindowFor(model string) int {
	if c.ModelContextMapping != nil {
		if v, ok := c.ModelContextMapping[model]; ok {
			return v
		}
	}
	return defaultContextWindow
}
