package codebase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"gopkg.in/yaml.v3"
)

const (
	CocoPluginsDirEnv = "CECE_COCO_PLUGINS_DIR"
	DefaultBaseURL    = "https://codebase-api.byted.org/v2/api/2022-06-01/LLMProxy/TraeV2"
	DefaultAuthHelper = "bytedcli auth get-codebase-jwt-token"
)

type cocoPluginFile struct {
	Models []cocoPluginModel `yaml:"models"`
}

type cocoPluginModel struct {
	Name          string         `yaml:"name"`
	ContextWindow int            `yaml:"context_window"`
	PromptHint    string         `yaml:"prompt_hint"`
	MergeSystemPrompt bool     `yaml:"merge_system_prompt"`
	BytedTrae     *cocoBytedTrae `yaml:"byted_trae"`
}

type cocoBytedTrae struct {
	BaseURL    string `yaml:"base_url"`
	ConfigName string `yaml:"config_name"`
	APIKey     string `yaml:"api_key"`
	Headers    map[string]string `yaml:"headers"`
	MaxTokens  int `yaml:"max_tokens"`
	Model      string `yaml:"model"`
}

func DefaultCocoPluginsDir() string {
	if dir := strings.TrimSpace(os.Getenv(CocoPluginsDirEnv)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches", "coco", "plugins")
	}
	return filepath.Join(home, ".cache", "coco", "plugins")
}

func DiscoverCocoPluginModels() ([]agent.ModelInfo, error) {
	return DiscoverCocoPluginModelsFromDir(DefaultCocoPluginsDir())
}

func DiscoverCocoPluginModelsFromDir(dir string) ([]agent.ModelInfo, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("coco plugin directory is not configured")
	}
	files, err := cocoPluginConfigFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no coco plugin configs found in %s", dir)
	}

	seen := make(map[string]struct{})
	var models []agent.ModelInfo
	for _, file := range files {
		ms, err := readCocoPluginModels(file)
		if err != nil {
			continue
		}
		for _, m := range ms {
			key := m.ID + "\x00" + m.ConfigName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no byted_trae models found in %s", dir)
	}
	return models, nil
}

func cocoPluginConfigFiles(dir string) ([]string, error) {
	if st, err := os.Stat(dir); err != nil {
		return nil, err
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	var files []string
	for _, name := range []string{"coco.yaml", "traecli.yaml"} {
		matches, err := filepath.Glob(filepath.Join(dir, "*", name))
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	sort.Strings(files)
	return files, nil
}

func readCocoPluginModels(path string) ([]agent.ModelInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg cocoPluginFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	var models []agent.ModelInfo
	for _, m := range cfg.Models {
		if m.BytedTrae == nil {
			continue
		}
		id := strings.TrimSpace(m.BytedTrae.Model)
		configName := strings.TrimSpace(m.BytedTrae.ConfigName)
		if id == "" || configName == "" {
			continue
		}
		displayName := strings.TrimSpace(m.Name)
		if displayName == "" {
			displayName = id
		}
		models = append(models, agent.ModelInfo{
			ID:               id,
			DisplayName:      displayName,
			MaxContextWindow: m.ContextWindow,
			BaseURL:          normalizeBaseURL(m.BytedTrae.BaseURL),
			Protocol:         "codebase",
			ConfigName:       configName,
			PromptHint:       m.PromptHint,
			Headers:          m.BytedTrae.Headers,
			APIKey:           m.BytedTrae.APIKey,
		})
	}
	return models, nil
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL = strings.TrimRight(strings.TrimSuffix(baseURL, "/chat/completions"), "/")
	}
	return baseURL
}
