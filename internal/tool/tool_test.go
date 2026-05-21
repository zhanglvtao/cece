package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Bash ──────────────────────────────────────────────────────────────────────

func TestBashToolInfo(t *testing.T) {
	var b bashTool
	info := b.Info()
	if info.Name != "Bash" {
		t.Fatalf("Name = %q, want Bash", info.Name)
	}
	if info.InputSchema == nil {
		t.Fatal("InputSchema is nil")
	}
}

func TestBashToolRunEcho(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{Command: "echo hello"})
	result := b.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("Content = %q, want to contain 'hello'", result.Content)
	}
}

func TestBashToolRunStreaming(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{Command: "echo line1; echo line2"})
	var lines []string
	emitter := &testEmitter{lines: &lines}
	result := b.Run(context.Background(), input, emitter)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if len(lines) < 2 {
		t.Fatalf("emitted %d lines, want >= 2: %v", len(lines), lines)
	}
	if lines[0] != "line1" {
		t.Fatalf("first emitted line = %q, want %q", lines[0], "line1")
	}
}

func TestBashToolRunFails(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{Command: "false"})
	result := b.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for exit code 1")
	}
}

func TestBashToolMissingCommand(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{})
	result := b.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing command")
	}
}

// ── Read ──────────────────────────────────────────────────────────────────────

func TestReadToolInfo(t *testing.T) {
	var r readTool
	info := r.Info()
	if info.Name != "Read" {
		t.Fatalf("Name = %q, want Read", info.Name)
	}
}

func TestReadToolRunFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello\nworld\n"), 0o644)

	var r readTool
	input, _ := json.Marshal(readParams{FilePath: path})
	result := r.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "hello") || !strings.Contains(result.Content, "world") {
		t.Fatalf("Content = %q, want to contain 'hello' and 'world'", result.Content)
	}
}

func TestReadToolRunMissingFile(t *testing.T) {
	var r readTool
	input, _ := json.Marshal(readParams{FilePath: "/nonexistent/file.txt"})
	result := r.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing file")
	}
}

func TestReadToolRunWithEmitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("content\n"), 0o644)

	var r readTool
	input, _ := json.Marshal(readParams{FilePath: path})
	var lines []string
	emitter := &testEmitter{lines: &lines}
	result := r.Run(context.Background(), input, emitter)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if len(lines) == 0 {
		t.Fatal("expected at least one emitted line")
	}
	if !strings.Contains(lines[0], "Reading") {
		t.Fatalf("first emitted line = %q, want to contain 'Reading'", lines[0])
	}
}

// ── Write ─────────────────────────────────────────────────────────────────────

func TestWriteToolInfo(t *testing.T) {
	var w writeTool
	info := w.Info()
	if info.Name != "Write" {
		t.Fatalf("Name = %q, want Write", info.Name)
	}
}

func TestWriteToolRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	var w writeTool
	input, _ := json.Marshal(writeParams{FilePath: path, Content: "hello"})
	result := w.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file content = %q, want %q", string(data), "hello")
	}
}

func TestWriteToolCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "out.txt")

	var w writeTool
	input, _ := json.Marshal(writeParams{FilePath: path, Content: "deep"})
	result := w.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "deep" {
		t.Fatalf("file content = %q, want %q", string(data), "deep")
	}
}

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistryGet(t *testing.T) {
	var b bashTool
	r := NewRegistry(b)
	tool, ok := r.Get("Bash")
	if !ok {
		t.Fatal("Get(Bash) not found")
	}
	if tool.Info().Name != "Bash" {
		t.Fatalf("Name = %q, want Bash", tool.Info().Name)
	}
	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("Get(nonexistent) should return false")
	}
}

func TestRegistryDefinitions(t *testing.T) {
	var b bashTool
	var r readTool
	reg := NewRegistry(b, r)
	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("len(Definitions) = %d, want 2", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["Bash"] || !names["Read"] {
		t.Fatalf("Definitions missing names, got %v", names)
	}
}

func TestRegistryExecuteUnknown(t *testing.T) {
	r := NewRegistry()
	result := r.Execute(context.Background(), "Nope", json.RawMessage(`{}`), nil)
	if !result.IsError {
		t.Fatal("expected IsError for unknown tool")
	}
}

// ── Glob ──────────────────────────────────────────────────────────────────────

func TestGlobToolInfo(t *testing.T) {
	var g globTool
	info := g.Info()
	if info.Name != "Glob" {
		t.Fatalf("Name = %q, want Glob", info.Name)
	}
}

func TestGlobToolRun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte(""), 0o644)

	var g globTool
	input, _ := json.Marshal(globParams{Pattern: "*.go", Path: dir})
	result := g.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "a.go") || !strings.Contains(result.Content, "b.go") {
		t.Fatalf("Content = %q, want to contain a.go and b.go", result.Content)
	}
	if strings.Contains(result.Content, "c.txt") {
		t.Fatalf("Content = %q, should not contain c.txt", result.Content)
	}
}

func TestGlobToolNoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0o644)

	var g globTool
	input, _ := json.Marshal(globParams{Pattern: "*.go", Path: dir})
	result := g.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "No files found") {
		t.Fatalf("Content = %q, want to contain 'No files found'", result.Content)
	}
}

func TestGlobToolMissingPattern(t *testing.T) {
	var g globTool
	input, _ := json.Marshal(globParams{})
	result := g.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing pattern")
	}
}

// ── Grep ──────────────────────────────────────────────────────────────────────

func TestGrepToolInfo(t *testing.T) {
	var g grepTool
	info := g.Info()
	if info.Name != "Grep" {
		t.Fatalf("Name = %q, want Grep", info.Name)
	}
}

func TestGrepToolRun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\nfoo bar\nhello Go\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("no match here\n"), 0o644)

	var g grepTool
	input, _ := json.Marshal(grepParams{Pattern: "hello", Path: dir})
	result := g.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "Found 2 matches") {
		t.Fatalf("Content = %q, want to contain 'Found 2 matches'", result.Content)
	}
	if !strings.Contains(result.Content, "a.txt") {
		t.Fatalf("Content = %q, want to contain 'a.txt'", result.Content)
	}
}

func TestGrepToolNoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644)

	var g grepTool
	input, _ := json.Marshal(grepParams{Pattern: "nonexistent", Path: dir})
	result := g.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "No files found") {
		t.Fatalf("Content = %q, want to contain 'No files found'", result.Content)
	}
}

func TestGrepToolWithInclude(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nfunc hello() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello from txt\n"), 0o644)

	var g grepTool
	input, _ := json.Marshal(grepParams{Pattern: "hello", Path: dir, Include: "*.go"})
	result := g.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "a.go") {
		t.Fatalf("Content = %q, want to contain 'a.go'", result.Content)
	}
	if strings.Contains(result.Content, "a.txt") {
		t.Fatalf("Content = %q, should not contain 'a.txt'", result.Content)
	}
}

func TestGrepToolMissingPattern(t *testing.T) {
	var g grepTool
	input, _ := json.Marshal(grepParams{})
	result := g.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing pattern")
	}
}

func TestGrepToolInvalidRegex(t *testing.T) {
	var g grepTool
	input, _ := json.Marshal(grepParams{Pattern: "[invalid"})
	result := g.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for invalid regex")
	}
}

// ── Edit ──────────────────────────────────────────────────────────────────────

func TestEditToolInfo(t *testing.T) {
	var e editTool
	info := e.Info()
	if info.Name != "Edit" {
		t.Fatalf("Name = %q, want Edit", info.Name)
	}
}

func TestEditToolReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\nfoo bar\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:  path,
		OldString: "world",
		NewString: "Go",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello Go\nfoo bar\n" {
		t.Fatalf("file content = %q, want %q", string(data), "hello Go\nfoo bar\n")
	}
	// Result should contain unified diff
	if !strings.Contains(result.Content, "--- a/") || !strings.Contains(result.Content, "+++ b/") {
		t.Fatalf("result should contain unified diff headers, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "-hello world") || !strings.Contains(result.Content, "+hello Go") {
		t.Fatalf("result should contain diff lines, got: %q", result.Content)
	}
}

func TestEditToolNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:  path,
		OldString: "nonexistent",
		NewString: "replacement",
	})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true when old_string not found")
	}
}

func TestEditToolMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("aaa\nbbb\naaa\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:  path,
		OldString: "aaa",
		NewString: "ccc",
	})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true when old_string appears multiple times")
	}
}

func TestEditToolReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("aaa\nbbb\naaa\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:   path,
		OldString:  "aaa",
		NewString:  "ccc",
		ReplaceAll: true,
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "ccc\nbbb\nccc\n" {
		t.Fatalf("file content = %q, want %q", string(data), "ccc\nbbb\nccc\n")
	}
}

func TestEditToolCreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:  path,
		OldString: "",
		NewString: "created",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "created" {
		t.Fatalf("file content = %q, want %q", string(data), "created")
	}
}

func TestEditToolDeleteContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello\nworld\ngoodbye\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		FilePath:  path,
		OldString: "world\n",
		NewString: "",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello\ngoodbye\n" {
		t.Fatalf("file content = %q, want %q", string(data), "hello\ngoodbye\n")
	}
}

func TestEditToolMissingFilePath(t *testing.T) {
	var e editTool
	input, _ := json.Marshal(editParams{OldString: "x", NewString: "y"})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing file_path")
	}
}

func TestEditToolMissingBothStrings(t *testing.T) {
	var e editTool
	input, _ := json.Marshal(editParams{FilePath: "/some/file.txt"})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true when both old_string and new_string are empty")
	}
}

// ── Diff ──────────────────────────────────────────────────────────────────────

func TestUnifiedDiff(t *testing.T) {
	old := "line1\nline2\nline3\n"
	new := "line1\nline2_changed\nline3\nline4\n"
	diff := UnifiedDiff("test.txt", "test.txt", old, new)

	if !strings.Contains(diff, "--- a/test.txt") {
		t.Fatalf("missing --- header in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+++ b/test.txt") {
		t.Fatalf("missing +++ header in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "-line2") {
		t.Fatalf("missing -line2 in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+line2_changed") {
		t.Fatalf("missing +line2_changed in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "+line4") {
		t.Fatalf("missing +line4 in diff:\n%s", diff)
	}
	if !strings.Contains(diff, "@@") {
		t.Fatalf("missing hunk header in diff:\n%s", diff)
	}
}

func TestUnifiedDiffIdentical(t *testing.T) {
	content := "same\ncontent\n"
	diff := UnifiedDiff("f.txt", "f.txt", content, content)
	if diff != "--- a/f.txt\n+++ b/f.txt\n" {
		t.Fatalf("expected no hunks for identical content, got:\n%s", diff)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

type testEmitter struct {
	lines *[]string
}

func (e *testEmitter) Emit(text string) {
	*e.lines = append(*e.lines, text)
}
