package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	defaultReadLimit = 2000 // lines
	maxReadFileLen   = 30000
)

type readParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"` // 1-based line number
	Limit  int    `json:"limit,omitempty"`
}

type readTool struct {
	tracker *ReadTracker
}

func NewRead(tracker *ReadTracker) Tool { return readTool{tracker: tracker} }

func (readTool) Effect() Effect { return EffectRead }

func (readTool) Info() Definition {
	return Definition{
		Name:        "Read",
		Description: "Read a file's contents with line numbers.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to read",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Line number to start reading from (1-based)",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Number of lines to read",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t readTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p readParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.Path == "" {
		return Result{Content: "missing path", IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Reading %s...", p.Path))
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return Result{Content: fmt.Sprintf("read: %v", err), IsError: true}
	}
	t.tracker.MarkRead(p.Path)

	content := string(data)
	lines := strings.Split(content, "\n")

	// Apply offset (1-based → 0-based)
	start := 0
	if p.Offset > 0 {
		start = p.Offset - 1
		if start >= len(lines) {
			return Result{Content: "offset past end of file", IsError: true}
		}
	}

	// Apply limit
	limit := defaultReadLimit
	if p.Limit > 0 {
		limit = p.Limit
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}

	// Format with line numbers
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%6d| %s\n", i+1, lines[i])
	}

	result := b.String()
	if len(result) > maxReadFileLen {
		result = truncateOutput(result)
	}

	return Result{Content: result}
}
