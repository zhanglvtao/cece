package aiden

import (
	"encoding/json"
	"testing"

	"github.com/zhanglvtao/cece/internal/tool"
)

func TestConvertSingleTool(t *testing.T) {
	tools := []tool.Definition{
		{
			Name:        "Bash",
			Description: "Run a shell command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		},
	}

	result := ConvertTools(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	got := result[0]
	if got.Type != "function" {
		t.Errorf("expected type 'function', got %q", got.Type)
	}
	if got.Function.Name != "Bash" {
		t.Errorf("expected name 'Bash', got %q", got.Function.Name)
	}
	if got.Function.Description != "Run a shell command" {
		t.Errorf("expected description 'Run a shell command', got %q", got.Function.Description)
	}
	if got.Function.Parameters == nil {
		t.Error("expected non-nil parameters")
	}
}

func TestConvertMultipleTools(t *testing.T) {
	tools := []tool.Definition{
		{Name: "Bash", Description: "Run bash", InputSchema: map[string]any{"type": "object"}},
		{Name: "Read", Description: "Read file", InputSchema: map[string]any{"type": "object"}},
		{Name: "Write", Description: "Write file", InputSchema: map[string]any{"type": "object"}},
	}

	result := ConvertTools(tools)
	if len(result) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result))
	}
	names := []string{result[0].Function.Name, result[1].Function.Name, result[2].Function.Name}
	expected := []string{"Bash", "Read", "Write"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("tool[%d]: expected %q, got %q", i, name, names[i])
		}
	}
}

func TestConvertEmptyTools(t *testing.T) {
	result := ConvertTools(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestConvertToolJSONFormat(t *testing.T) {
	tools := []tool.Definition{
		{
			Name:        "Bash",
			Description: "Run a command",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		},
	}

	result := ConvertTools(tools)
	data, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "function" {
		t.Errorf("expected type 'function', got %v", parsed["type"])
	}
	fn := parsed["function"].(map[string]any)
	if fn["name"] != "Bash" {
		t.Errorf("expected name 'Bash', got %v", fn["name"])
	}
	if fn["parameters"] == nil {
		t.Error("expected non-nil parameters")
	}
}
