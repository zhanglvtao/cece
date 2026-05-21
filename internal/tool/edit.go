package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type editParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type editTool struct{}

func NewEdit() Tool { return editTool{} }

func (editTool) Info() Definition {
	return Definition{
		Name:        "Edit",
		Description: "Make precise string replacements in files. Returns a unified diff of changes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to edit",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "The text to find in the file. Must be an exact match and unique unless replace_all is true. If empty, creates a new file.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "The text to replace old_string with. If empty, deletes old_string.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace all occurrences of old_string (default: false, requires unique match)",
				},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}
}

func (editTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p editParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.FilePath == "" {
		return Result{Content: "missing file_path", IsError: true}
	}
	if p.OldString == "" && p.NewString == "" {
		return Result{Content: "old_string and new_string are both empty", IsError: true}
	}

	// Create mode: old_string is empty → create new file
	if p.OldString == "" {
		if emitter != nil {
			emitter.Emit(fmt.Sprintf("Creating %s...", p.FilePath))
		}
		return editCreate(p.FilePath, p.NewString)
	}

	// Read existing file
	oldContent, err := os.ReadFile(p.FilePath)
	if err != nil {
		return Result{Content: fmt.Sprintf("read: %v", err), IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Editing %s...", p.FilePath))
	}

	s := string(oldContent)

	if p.ReplaceAll {
		count := strings.Count(s, p.OldString)
		if count == 0 {
			return Result{Content: "old_string not found in file", IsError: true}
		}
		newContent := strings.ReplaceAll(s, p.OldString, p.NewString)
		diff := UnifiedDiff(p.FilePath, p.FilePath, s, newContent)
		if err := os.WriteFile(p.FilePath, []byte(newContent), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
		}
		return Result{Content: diff}
	}

	// Single replacement: must be unique
	idx := strings.Index(s, p.OldString)
	if idx < 0 {
		return Result{Content: "old_string not found in file", IsError: true}
	}
	lastIdx := strings.LastIndex(s, p.OldString)
	if idx != lastIdx {
		return Result{Content: "old_string appears multiple times — use replace_all or provide more context to make it unique", IsError: true}
	}

	newContent := s[:idx] + p.NewString + s[idx+len(p.OldString):]
	diff := UnifiedDiff(p.FilePath, p.FilePath, s, newContent)
	if err := os.WriteFile(p.FilePath, []byte(newContent), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
	}
	return Result{Content: diff}
}

func editCreate(path, content string) Result {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return Result{Content: "file already exists — use old_string to edit it", IsError: true}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{Content: fmt.Sprintf("mkdir: %v", err), IsError: true}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
	}

	diff := UnifiedDiff(path, path, "", content)
	return Result{Content: diff}
}
