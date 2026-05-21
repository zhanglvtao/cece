package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type writeParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type writeTool struct{}

func NewWrite() Tool { return writeTool{} }

func (writeTool) Info() Definition {
	return Definition{
		Name:        "Write",
		Description: "Create or overwrite a file with the given content.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to write to the file",
				},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

func (writeTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p writeParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.FilePath == "" {
		return Result{Content: "missing file_path", IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Writing %s...", p.FilePath))
	}

	// Create parent directories if needed.
	dir := filepath.Dir(p.FilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{Content: fmt.Sprintf("mkdir: %v", err), IsError: true}
	}

	if err := os.WriteFile(p.FilePath, []byte(p.Content), 0o644); err != nil {
		return Result{Content: fmt.Sprintf("write: %v", err), IsError: true}
	}

	return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.FilePath)}
}
