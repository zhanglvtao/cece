package ui

import (
	"strings"
	"testing"
)

func TestFormatToolArgs(t *testing.T) {
	tests := []struct {
		toolName     string
		args         map[string]any
		want         string
		wantContains []string // if set, verify each substring is present (for non-deterministic map order)
	}{
		{
			toolName: "Bash",
			args:     map[string]any{"command": "ls -la", "timeout": float64(120)},
			want:     "ls -la",
		},
		{
			toolName: "Bash",
			args:     map[string]any{"command": "echo hello"},
			want:     "echo hello",
		},
		{
			toolName: "Read",
			args:     map[string]any{"file_path": "/foo/bar.go", "offset": float64(10), "limit": float64(20)},
			want:     "/foo/bar.go  offset:10  limit:20",
		},
		{
			toolName: "Read",
			args:     map[string]any{"file_path": "/foo/bar.go"},
			want:     "/foo/bar.go",
		},
		{
			toolName: "Edit",
			args:     map[string]any{"file_path": "/foo/bar.go", "old_string": "hello", "new_string": "world"},
			want:     "/foo/bar.go",
		},
		{
			toolName: "Edit",
			args:     map[string]any{"file_path": "/foo/bar.go", "old_string": "hello", "new_string": "world", "replace_all": true},
			want:     "/foo/bar.go  replace_all:true",
		},
		{
			toolName: "Write",
			args:     map[string]any{"file_path": "/foo/bar.go", "content": "package main\n"},
			want:     "/foo/bar.go",
		},
		{
			toolName: "Glob",
			args:     map[string]any{"pattern": "*.go", "path": "/src"},
			want:     "*.go  path:/src",
		},
		{
			toolName: "Glob",
			args:     map[string]any{"pattern": "*.go"},
			want:     "*.go",
		},
		{
			toolName: "Grep",
			args:     map[string]any{"pattern": "hello", "include": "*.go", "path": "/src"},
			want:     "hello  include:*.go  path:/src",
		},
		{
			toolName: "Grep",
			args:     map[string]any{"pattern": "hello"},
			want:     "hello",
		},
		{
			toolName: "CustomTool",
			args:     map[string]any{"query": "test", "limit": float64(5)},
			// genericArgs: map iteration order is non-deterministic,
			// so we verify both keys are present with correct values.
			wantContains: []string{"query:test", "limit:5"},
		},
		{
			toolName: "Bash",
			args:     nil,
			want:     "",
		},
		{
			toolName: "Bash",
			args:     map[string]any{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := formatToolArgs(tt.toolName, tt.args)
			if len(tt.wantContains) > 0 {
				for _, sub := range tt.wantContains {
					if !strings.Contains(got, sub) {
						t.Errorf("formatToolArgs(%q, %v) = %q, want to contain %q", tt.toolName, tt.args, got, sub)
					}
				}
			} else if got != tt.want {
				t.Errorf("formatToolArgs(%q, %v) = %q, want %q", tt.toolName, tt.args, got, tt.want)
			}
		})
	}
}
