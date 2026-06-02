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

// LintConfig maps file extensions (without dot, e.g. "go", "ts") to shell
// command templates. The placeholder {file} is replaced with the absolute
// path of the file being written.
type LintConfig map[string]string

type Config struct {
	Model               string
	LightModel          string           // lightweight model for title generation etc.
	Debug               bool
	Yolo                bool
	MaxTokens           int
	DefaultMode         string           // "default", "auto-accept", or "plan"
	ModelContextMapping map[string]int   // model ID -> max context window
	Providers           []ProviderConfig
	MCP                 MCPs
	Lint                LintConfig
}

type settingsFile struct {
	Provider struct {
		Model               string           `json:"model"`
		LightModel          string           `json:"lightModel"`
		MaxTokens           int              `json:"maxTokens"`
		ModelContextMapping map[string]int   `json:"modelContextMapping"`
		Providers           []ProviderConfig `json:"providers"`
	} `json:"provider"`
	DefaultMode struct {
		Mode string `json:"mode"` // "default", "auto-accept", or "plan"
	} `json:"defaultMode"`
	Debug struct {
		Enabled bool `json:"enabled"`
	} `json:"debug"`
	Yolo struct {
		Enabled bool `json:"enabled"`
	} `json:"yolo"`
	MCP MCPs `json:"mcp"`
	Lint LintConfig `json:"lint"`
}

func Load(projectDir string) (Config, error) {
	cfg := Config{
	}

	sf := loadSettingsFiles(projectDir)
	cfg.Model = strings.TrimSpace(sf.Provider.Model)
	cfg.LightModel = strings.TrimSpace(sf.Provider.LightModel)
	cfg.MaxTokens = sf.Provider.MaxTokens
	cfg.DefaultMode = sf.DefaultMode.Mode
	cfg.ModelContextMapping = sf.Provider.ModelContextMapping
	cfg.Providers = sf.Provider.Providers
	cfg.Debug = sf.Debug.Enabled
	cfg.Yolo = sf.Yolo.Enabled
	cfg.MCP = sf.MCP
	cfg.Lint = sf.Lint

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

// loadSettingsFiles reads both project-level and user-level settings files,
// then merges them per-field with project taking priority.
func loadSettingsFiles(projectDir string) settingsFile {
	var project, user settingsFile

	projectPath := filepath.Join(projectDir, settingsRelPath)
	if data, err := os.ReadFile(projectPath); err == nil {
		json.Unmarshal(data, &project) //nolint:errcheck // best-effort, validated later
	}

	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, settingsRelPath)
		if data, err := os.ReadFile(globalPath); err == nil {
			json.Unmarshal(data, &user) //nolint:errcheck // best-effort
		}
	}

	return mergeSettings(project, user)
}

// mergeSettings combines two settingsFile values per-field.
// Project takes priority; user fills in missing fields.
func mergeSettings(project, user settingsFile) settingsFile {
	var out settingsFile

	// Provider: scalar fields — project wins if non-zero
	if strings.TrimSpace(project.Provider.Model) != "" {
		out.Provider.Model = project.Provider.Model
	} else {
		out.Provider.Model = user.Provider.Model
	}
	if strings.TrimSpace(project.Provider.LightModel) != "" {
		out.Provider.LightModel = project.Provider.LightModel
	} else {
		out.Provider.LightModel = user.Provider.LightModel
	}
	if project.Provider.MaxTokens != 0 {
		out.Provider.MaxTokens = project.Provider.MaxTokens
	} else {
		out.Provider.MaxTokens = user.Provider.MaxTokens
	}
	// DefaultMode: project wins if non-empty
	if strings.TrimSpace(project.DefaultMode.Mode) != "" {
		out.DefaultMode.Mode = project.DefaultMode.Mode
	} else {
		out.DefaultMode.Mode = user.DefaultMode.Mode
	}

	// ModelContextMapping: merge maps, project keys win
	out.Provider.ModelContextMapping = mergeMap(project.Provider.ModelContextMapping, user.Provider.ModelContextMapping)

	// Providers: project wins if non-empty, otherwise user
	if len(project.Provider.Providers) > 0 {
		out.Provider.Providers = project.Provider.Providers
	} else {
		out.Provider.Providers = user.Provider.Providers
	}

	// Debug: project wins if explicitly enabled
	if project.Debug.Enabled {
		out.Debug.Enabled = true
	} else {
		out.Debug.Enabled = user.Debug.Enabled
	}

	// Yolo: project wins if explicitly enabled
	if project.Yolo.Enabled {
		out.Yolo.Enabled = true
	} else {
		out.Yolo.Enabled = user.Yolo.Enabled
	}

	// MCP: merge maps, project keys win
	out.MCP = mergeMap(project.MCP, user.MCP)

	// Lint: merge maps, project keys win
	out.Lint = mergeMap(project.Lint, user.Lint)

	return out
}

func nonZeroInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func mergeMap[K comparable, V any](a, b map[K]V) map[K]V {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[K]V, len(b)+len(a))
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		out[k] = v
	}
	return out
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
