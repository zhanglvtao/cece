package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runTool(t *testing.T, tl Tool, params map[string]any) Result {
	t.Helper()
	input, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return tl.Run(context.Background(), input, nil)
}

func TestEditGuardRejectsUnreadExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracker := NewReadTracker()
	edit := NewEdit(tracker)

	res := runTool(t, edit, map[string]any{"path": path, "old_string": "hello", "new_string": "hi"})
	if !res.IsError {
		t.Fatalf("editing unread file should error, got: %q", res.Content)
	}

	// After marking read, the edit succeeds.
	tracker.MarkRead(path)
	res = runTool(t, edit, map[string]any{"path": path, "old_string": "hello", "new_string": "hi"})
	if res.IsError {
		t.Fatalf("editing read file should succeed, got error: %q", res.Content)
	}
}

func TestEditCreateModeBypassesGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	edit := NewEdit(NewReadTracker())

	// old_string empty => create mode, no prior Read required.
	res := runTool(t, edit, map[string]any{"path": path, "old_string": "", "new_string": "content"})
	if res.IsError {
		t.Fatalf("create-mode edit should succeed, got error: %q", res.Content)
	}
}

func TestWriteGuardRejectsUnreadExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracker := NewReadTracker()
	write := NewWrite(tracker)

	res := runTool(t, write, map[string]any{"path": path, "content": "new"})
	if !res.IsError {
		t.Fatalf("overwriting unread file should error, got: %q", res.Content)
	}

	tracker.MarkRead(path)
	res = runTool(t, write, map[string]any{"path": path, "content": "new"})
	if res.IsError {
		t.Fatalf("overwriting read file should succeed, got error: %q", res.Content)
	}
}

func TestWriteNewFileBypassesGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	write := NewWrite(NewReadTracker())

	res := runTool(t, write, map[string]any{"path": path, "content": "hello"})
	if res.IsError {
		t.Fatalf("writing new file should succeed, got error: %q", res.Content)
	}
}
