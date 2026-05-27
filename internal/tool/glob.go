package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxGlobResults = 100

type globParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type globTool struct{}

func NewGlob() Tool { return globTool{} }

func (globTool) Effect() Effect { return EffectRead }

func (globTool) Info() Definition {
	return Definition{
		Name:        "Glob",
		Description: "Search for files by name pattern.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": `The glob pattern to match files (e.g. "*.go", "**/*.ts", "*.{ts,tsx}")`,
				},
				"path": map[string]any{
					"type":        "string",
					"description": "The directory to search in. Defaults to the current working directory.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (globTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p globParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.Pattern == "" {
		return Result{Content: "missing pattern", IsError: true}
	}

	re, err := regexp.Compile(globToRegex(p.Pattern))
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid pattern: %v", err), IsError: true}
	}

	searchPath := p.Path
	if searchPath == "" {
		searchPath, _ = os.Getwd()
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Searching for %q in %s...", p.Pattern, searchPath))
	}

	var matches []string
	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if info.IsDir() {
			if isHiddenDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(searchPath, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if re.MatchString(rel) {
			matches = append(matches, filepath.ToSlash(path))
			if len(matches) >= maxGlobResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return Result{Content: fmt.Sprintf("walk: %v", err), IsError: true}
	}

	if len(matches) == 0 {
		return Result{Content: "No files found"}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d files\n", len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "  %s\n", m)
	}

	return Result{Content: truncateOutput(b.String())}
}
