package traecli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zhanglvtao/cece/internal/agent"
)

// CocoPluginsDir is the model cache directory used by LoadDefaultLocalModels.
var CocoPluginsDir = DefaultCocoPluginsDir()

// DefaultCocoPluginsDir returns the local coco plugin cache directory that
// contains model definitions installed by traecli/coco.
func DefaultCocoPluginsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Caches", "coco", "plugins")
}

// LoadDefaultLocalModels reads model definitions from the default coco plugin cache.
func LoadDefaultLocalModels() ([]agent.ModelInfo, error) {
	return LoadLocalModels(CocoPluginsDir)
}

type cocoPluginFile struct {
	Models []cocoModel `yaml:"models"`
}

type cocoModel struct {
	Name          string `yaml:"name"`
	ContextWindow int    `yaml:"context_window"`
	BytedTrae     struct {
		ConfigName string `yaml:"config_name"`
		Model      string `yaml:"model"`
	} `yaml:"byted_trae"`
}

// LoadLocalModels reads coco plugin model definitions from root. It only reads
// coco.yaml files because traecli.yaml files can describe wrapper plugins such
// as aime_pc_llm_proxy_plugin rather than user-selectable models.
func LoadLocalModels(root string) ([]agent.ModelInfo, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read coco plugins dir: %w", err)
	}

	var out []agent.ModelInfo
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "coco.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var file cocoPluginFile
		if err := yaml.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, m := range file.Models {
			name := strings.TrimSpace(m.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, agent.ModelInfo{
				ID:               name,
				DisplayName:      name,
				MaxContextWindow: m.ContextWindow,
				ConfigName:       strings.TrimSpace(m.BytedTrae.ConfigName),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
