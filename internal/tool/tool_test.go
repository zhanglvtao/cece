package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	properties, ok := info.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want map[string]any", info.InputSchema["properties"])
	}
	timeout, ok := properties["timeout"].(map[string]any)
	if !ok {
		t.Fatalf("timeout schema = %T, want map[string]any", properties["timeout"])
	}
	if timeout["default"] != 10 {
		t.Fatalf("timeout default = %v, want 10", timeout["default"])
	}
	if timeout["minimum"] != 0 {
		t.Fatalf("timeout minimum = %v, want 0", timeout["minimum"])
	}
	if timeout["description"] != "Optional timeout in seconds. Defaults to 10 when omitted or set to 0." {
		t.Fatalf("timeout description = %v, want updated description", timeout["description"])
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

func TestResolveBashTimeoutSeconds(t *testing.T) {
	tests := []struct {
		name    string
		timeout int
		want    int
		wantErr bool
	}{
		{name: "zero uses default", timeout: 0, want: 10},
		{name: "positive uses explicit value", timeout: 5, want: 5},
		{name: "negative is rejected", timeout: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBashTimeoutSeconds(tt.timeout)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveBashTimeoutSeconds(%d) error = nil, want error", tt.timeout)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveBashTimeoutSeconds(%d) error = %v", tt.timeout, err)
			}
			if got != tt.want {
				t.Fatalf("resolveBashTimeoutSeconds(%d) = %d, want %d", tt.timeout, got, tt.want)
			}
		})
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

func TestBashToolRunRejectsNegativeTimeout(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{Command: "echo hello", Timeout: -1})
	result := b.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for negative timeout")
	}
	if !strings.Contains(result.Content, "invalid timeout") {
		t.Fatalf("Content = %q, want to contain invalid timeout", result.Content)
	}
}

func TestBashToolRunTimesOut(t *testing.T) {
	var b bashTool
	input, _ := json.Marshal(bashParams{Command: "sleep 2", Timeout: 1})
	start := time.Now()
	result := b.Run(context.Background(), input, nil)
	elapsed := time.Since(start)
	if !result.IsError {
		t.Fatal("IsError = false, want true for timeout")
	}
	if !strings.Contains(result.Content, "command timed out") {
		t.Fatalf("Content = %q, want timeout message", result.Content)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout did not take effect: elapsed %v, want < 3s", elapsed)
	}
}

func TestBashToolRunTimesOutDefault(t *testing.T) {
	var b bashTool
	// Default timeout is 10s; sleep 30 should be killed well before that.
	input, _ := json.Marshal(bashParams{Command: "sleep 30"})
	start := time.Now()
	result := b.Run(context.Background(), input, nil)
	elapsed := time.Since(start)
	if !result.IsError {
		t.Fatal("IsError = false, want true for timeout")
	}
	if !strings.Contains(result.Content, "command timed out") {
		t.Fatalf("Content = %q, want timeout message", result.Content)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("default timeout did not take effect: elapsed %v, want < 15s", elapsed)
	}
}

func TestBashToolRunTimesOutChildProcess(t *testing.T) {
	var b bashTool
	// bash spawns a child `sleep`; the process group kill should terminate it.
	input, _ := json.Marshal(bashParams{Command: "sleep 60", Timeout: 1})
	start := time.Now()
	result := b.Run(context.Background(), input, nil)
	elapsed := time.Since(start)
	if !result.IsError {
		t.Fatal("IsError = false, want true for timeout")
	}
	if !strings.Contains(result.Content, "command timed out") {
		t.Fatalf("Content = %q, want timeout message", result.Content)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("child process not killed with group: elapsed %v, want < 3s", elapsed)
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
	input, _ := json.Marshal(readParams{Path: path})
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
	input, _ := json.Marshal(readParams{Path: "/nonexistent/file.txt"})
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
	input, _ := json.Marshal(readParams{Path: path})
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
	input, _ := json.Marshal(writeParams{Path: path, Content: "hello"})
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
	if !strings.Contains(result.Content, "--- a/") || !strings.Contains(result.Content, "+++ b/") {
		t.Fatalf("result should contain unified diff headers, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "+hello") {
		t.Fatalf("result should contain written content diff, got: %q", result.Content)
	}
}

func TestWriteToolCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "out.txt")

	var w writeTool
	input, _ := json.Marshal(writeParams{Path: path, Content: "deep"})
	result := w.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "deep" {
		t.Fatalf("file content = %q, want %q", string(data), "deep")
	}
}

func TestPlanModeRemindersUseSystemReminderTags(t *testing.T) {
	fullReminder := BuildFullPlanReminder("/tmp/.cece/plans", false, DefaultPlanModeMockupAllowPattern)
	if !strings.Contains(fullReminder, "<system-reminder>") {
		t.Fatalf("full reminder = %q, want system-reminder tag", fullReminder)
	}
	if !strings.Contains(fullReminder, "You are already in plan mode.") {
		t.Fatalf("full reminder = %q, want explicit current plan mode state", fullReminder)
	}
	if !strings.Contains(fullReminder, DefaultPlanModeMockupAllowPattern) {
		t.Fatalf("full reminder = %q, want default mockup allow path", fullReminder)
	}
	if !strings.Contains(BuildSparsePlanReminder("/tmp/.cece/plans", false, DefaultPlanModeMockupAllowPattern), "<system-reminder>") {
		t.Fatalf("sparse reminder = %q, want system-reminder tag", BuildSparsePlanReminder("/tmp/.cece/plans", false, DefaultPlanModeMockupAllowPattern))
	}
	if !strings.Contains(ExitPlanModeReminder(), "<system-reminder>") {
		t.Fatalf("exit reminder = %q, want system-reminder tag", ExitPlanModeReminder())
	}
}

func TestPlanModeStateAllowsPlanAndMockupWrites(t *testing.T) {
	projectDir := t.TempDir()
	state := NewPlanModeState()
	state.SetProjectDir(projectDir)
	state.Enter()

	if !state.IsPlanModeWriteAllowed(filepath.Join(state.PlansDir(), "plan.md")) {
		t.Fatal("plan file should be allowed")
	}
	if !state.IsPlanModeWriteAllowed(filepath.Join(projectDir, ".superpowers", "brainstorm", "session", "content", "mockup.html")) {
		t.Fatal("visual companion mockup should be allowed")
	}
	if state.IsPlanModeWriteAllowed(filepath.Join(projectDir, "internal", "tool", "x.go")) {
		t.Fatal("source file should not be allowed in plan mode")
	}
}

func TestPlanModeStateAllowsConfiguredWritePatterns(t *testing.T) {
	projectDir := t.TempDir()
	state := NewPlanModeState()
	state.SetProjectDir(projectDir)
	state.SetPlanModeWriteAllowPatterns([]string{"docs/mockups/**"})
	state.Enter()

	if !state.IsPlanModeWriteAllowed(filepath.Join(projectDir, "docs", "mockups", "a.html")) {
		t.Fatal("configured allow pattern should be allowed")
	}
	if state.IsPlanModeWriteAllowed(filepath.Join(projectDir, "docs", "notes.md")) {
		t.Fatal("unconfigured docs path should not be allowed")
	}
}

func TestPlanModeStateRejectsEscapedAllowedPath(t *testing.T) {
	projectDir := t.TempDir()
	state := NewPlanModeState()
	state.SetProjectDir(projectDir)
	state.Enter()

	if state.IsPlanModeWriteAllowed(filepath.Join(projectDir, ".superpowers", "brainstorm", "session", "content", "..", "outside.html")) {
		t.Fatal("parent traversal should not be allowed")
	}
}

func TestExitPlanModeRejectsEmptyPlanFile(t *testing.T) {
	state := NewPlanModeState()
	state.SetProjectDir(t.TempDir())
	state.Enter()

	planFile := filepath.Join(state.PlansDir(), "empty.md")
	if err := os.WriteFile(planFile, []byte("\n\t  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"plan_file": planFile})
	result := NewExitPlanMode(state).Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatalf("IsError = false, content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "is empty") {
		t.Fatalf("content = %q, want empty plan error", result.Content)
	}
	if state.Mode() != PermissionModePlan {
		t.Fatalf("mode = %q, want plan", state.Mode())
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

func TestRegistryPersistsLargeBashOutput(t *testing.T) {
	r := NewRegistry(NewBash())
	r.SetResultStore(NewResultStore(t.TempDir()))
	input, _ := json.Marshal(bashParams{Command: "printf '%030001dTAIL' 0"})
	result := r.Execute(context.Background(), "Bash", input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if result.OutputPath == "" {
		t.Fatal("OutputPath is empty")
	}
	stored, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("read stored output: %v", err)
	}
	if !strings.Contains(string(stored), "TAIL") {
		t.Fatalf("stored output missing TAIL")
	}
	if !strings.Contains(result.Content, "Full output saved to:") {
		t.Fatalf("Content = %q, want saved output hint", result.Content)
	}
	if strings.Contains(result.Content, "TAIL") {
		t.Fatalf("Content should contain preview only, got TAIL")
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
		Path:      path,
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
		Path:      path,
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
		Path:      path,
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
		Path:       path,
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
		Path:      path,
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
		Path:      path,
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

func TestRegistryExecuteEditAllowsEmptyReplacementString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello\nworld\ngoodbye\n"), 0o644)

	r := NewRegistry(editTool{})
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "world\n",
		NewString: "",
	})
	result := r.Execute(context.Background(), "Edit", input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello\ngoodbye\n" {
		t.Fatalf("file content = %q, want %q", string(data), "hello\ngoodbye\n")
	}
}

func TestRegistryExecuteEditAllowsEmptyOldStringForCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	r := NewRegistry(editTool{})
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "",
		NewString: "created",
	})
	result := r.Execute(context.Background(), "Edit", input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "created" {
		t.Fatalf("file content = %q, want %q", string(data), "created")
	}
}

func TestEditToolMissingPath(t *testing.T) {
	var e editTool
	input, _ := json.Marshal(editParams{OldString: "x", NewString: "y"})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for missing path")
	}
}

func TestEditToolMissingBothStrings(t *testing.T) {
	var e editTool
	input, _ := json.Marshal(editParams{Path: "/some/file.txt"})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true when both old_string and new_string are empty")
	}
}

// ── Fuzzy matching ────────────────────────────────────────────────────────────

func TestEditFuzzyTabToSpace(t *testing.T) {
	// File has tab, LLM provides spaces in old_string
	// actualOld extracts the tab version from the file
	// new_string replaces the whole matched region
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte("\thello world\n\tfoo bar\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "    hello world\n", // 4 spaces instead of tab
		NewString: "    goodbye world\n",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// new_string is written as-is; the unmatched tab line is preserved
	if string(data) != "    goodbye world\n\tfoo bar\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditFuzzyCRLF(t *testing.T) {
	// File has CRLF, LLM provides LF in old_string
	// actualOld extracts the CRLF version from the file
	// new_string is written as-is (LF)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello\r\nworld\r\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "hello\nworld", // LF instead of CRLF
		NewString: "hello\nGo",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// new_string is written as-is; trailing CRLF from unmatched portion preserved
	if string(data) != "hello\nGo\r\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditFuzzyQuotes(t *testing.T) {
	// File has curly quotes, LLM provides straight quotes
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("\u201Chello\u201D\n"), 0o644) // "hello"

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: `"hello"`, // straight quotes
		NewString: `"goodbye"`,
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// actualOld is the curly-quoted version from file;
	// new_string replaces it with straight quotes as given
	if string(data) != "\"goodbye\"\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditFuzzyExactMatchPreferred(t *testing.T) {
	// When exact match exists, it should be used (no normalization needed)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "hello world",
		NewString: "hello Go",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello Go\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditFuzzyNotFoundWithEnhancedError(t *testing.T) {
	// When nothing matches, error should include file context
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "nonexistent",
		NewString: "replacement",
	})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "old_string not found") {
		t.Fatalf("error should mention old_string not found, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "line1") {
		t.Fatalf("error should include file context, got: %q", result.Content)
	}
}

func TestEditFuzzyTabUniquenessCheck(t *testing.T) {
	// Multiple tab matches should be detected for uniqueness
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("\tfoo\nbar\n\tfoo\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "    foo", // spaces matching tab
		NewString: "baz",
	})
	result := e.Run(context.Background(), input, nil)
	if !result.IsError {
		t.Fatal("IsError = false, want true for multiple matches")
	}
	if !strings.Contains(result.Content, "multiple times") {
		t.Fatalf("error should mention multiple times, got: %q", result.Content)
	}
}

func TestEditFuzzyReplaceAll(t *testing.T) {
	// replace_all with tab/space normalization
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("\tfoo\nmiddle\n\tfoo\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:       path,
		OldString:  "    foo", // spaces matching tab
		NewString:  "    bar",
		ReplaceAll: true,
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// actualOld is "\tfoo", new_string replaces it
	if string(data) != "    bar\nmiddle\n    bar\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditFuzzyCRLFReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo\r\nbar\r\nfoo\r\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:       path,
		OldString:  "foo", // LF-only old_string
		NewString:  "baz",
		ReplaceAll: true,
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "baz\r\nbar\r\nbaz\r\n" {
		t.Fatalf("file content = %q, want CRLF preserved", string(data))
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

// ── ValidateInput ──────────────────────────────────────────────────────────────

func TestValidateInputEmptyObject(t *testing.T) {
	var b bashTool
	result := validateInput(b.Info(), json.RawMessage(`{}`))
	if result == nil {
		t.Fatal("expected validation error for empty object")
	}
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "command") {
		t.Fatalf("error should mention missing field 'command', got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "Bash") {
		t.Fatalf("error should mention tool name 'Bash', got: %q", result.Content)
	}
}

func TestValidateInputMissingOneOfMany(t *testing.T) {
	var w writeTool
	result := validateInput(w.Info(), json.RawMessage(`{"path":"/tmp/x"}`))
	if result == nil {
		t.Fatal("expected validation error for missing content field")
	}
	if !strings.Contains(result.Content, "content") {
		t.Fatalf("error should mention missing field 'content', got: %q", result.Content)
	}
	if strings.Contains(result.Content, "path") {
		t.Fatalf("error should NOT mention 'path' since it was provided, got: %q", result.Content)
	}
}

func TestValidateInputAllPresent(t *testing.T) {
	var b bashTool
	result := validateInput(b.Info(), json.RawMessage(`{"command":"echo hi"}`))
	if result != nil {
		t.Fatalf("expected nil for valid input, got: %v", result)
	}
}

func TestValidateInputAcceptsEmptyStringForPresentRequiredField(t *testing.T) {
	var b bashTool
	result := validateInput(b.Info(), json.RawMessage(`{"command":""}`))
	// Single required field that is empty-string is treated as missing — this
	// catches codebase/aiden model artifacts where the model fails to fill in
	// the parameter value.
	if result == nil {
		t.Fatalf("expected error when sole required field is empty, got nil")
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got false")
	}
}

func TestValidateInputAllowsEmptyStringWhenOtherRequiredFieldPresent(t *testing.T) {
	// Edit tool: new_string="" is valid (delete semantics) when old_string and path are set.
	var e editTool
	result := validateInput(e.Info(), json.RawMessage(`{"path":"/tmp/x","old_string":"foo","new_string":""}`))
	if result != nil {
		t.Fatalf("expected nil when other required fields have content, got: %v", result)
	}
}

func TestValidateInputNoRequiredFields(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	def := Definition{Name: "NoReq", InputSchema: schema}
	result := validateInput(def, json.RawMessage(`{}`))
	if result != nil {
		t.Fatalf("expected nil for schema with no required fields, got: %v", result)
	}
}

func TestValidateInputMalformedJSON(t *testing.T) {
	var b bashTool
	result := validateInput(b.Info(), json.RawMessage(`not json`))
	if result == nil {
		t.Fatal("expected validation error for malformed JSON")
	}
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}
	if !strings.Contains(result.Content, "Invalid tool input JSON") {
		t.Fatalf("error should mention invalid JSON, got: %q", result.Content)
	}
}

func TestValidateInputErrorMessageFormat(t *testing.T) {
	var b bashTool
	result := validateInput(b.Info(), json.RawMessage(`{}`))
	if result == nil {
		t.Fatal("expected validation error")
	}
	// Should contain: tool name, field name, field type, field description
	if !strings.Contains(result.Content, "Bash") {
		t.Fatalf("missing tool name, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "command") {
		t.Fatalf("missing field name, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "string") {
		t.Fatalf("missing field type, got: %q", result.Content)
	}
	if !strings.Contains(result.Content, "bash command") {
		t.Fatalf("missing field description, got: %q", result.Content)
	}
}

func TestRegistryExecuteValidatesBeforeRun(t *testing.T) {
	var b bashTool
	r := NewRegistry(b)
	result := r.Execute(context.Background(), "Bash", json.RawMessage(`{}`), nil)
	if !result.IsError {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(result.Content, "Missing required parameter") {
		t.Fatalf("expected validation error, got: %q", result.Content)
	}
}

func TestGetRequiredFieldsFromMapAny(t *testing.T) {
	// When InputSchema is map[string]any, required comes as []any not []string
	schema := map[string]any{
		"required": []any{"command", "path"},
	}
	fields := getRequiredFields(schema)
	if len(fields) != 2 || fields[0] != "command" || fields[1] != "path" {
		t.Fatalf("got %v, want [command path]", fields)
	}
}

// ── PlanModeState: exitTargetMode & pendingModeReminder ──────────────────────

func TestExitWithExitTargetMode(t *testing.T) {
	s := NewPlanModeState()
	s.SetProjectDir(t.TempDir())
	s.Enter()
	if s.Mode() != PermissionModePlan {
		t.Fatalf("mode = %q, want plan", s.Mode())
	}
	// Set exit target to auto-accept before Exit
	s.SetExitTargetMode(PermissionModeAutoAccept)
	if !s.Exit() {
		t.Fatal("Exit() = false, want true")
	}
	if s.Mode() != PermissionModeAutoAccept {
		t.Fatalf("mode after Exit = %q, want auto-accept", s.Mode())
	}
}

func TestExitWithoutExitTargetMode(t *testing.T) {
	s := NewPlanModeState()
	s.SetProjectDir(t.TempDir())
	s.Enter()
	if !s.Exit() {
		t.Fatal("Exit() = false, want true")
	}
	if s.Mode() != PermissionModeDefault {
		t.Fatalf("mode after Exit = %q, want default", s.Mode())
	}
}

func TestSetModeSetsPendingReminder(t *testing.T) {
	s := NewPlanModeState()
	s.SetMode(PermissionModeAutoAccept)
	reminder := s.DrainModeReminder()
	if reminder == "" {
		t.Fatal("DrainModeReminder() = empty, want non-empty reminder")
	}
	if !strings.Contains(reminder, "auto-accept") {
		t.Fatalf("reminder = %q, want to contain 'auto-accept'", reminder)
	}
}

func TestSetModeSameModeNoReminder(t *testing.T) {
	s := NewPlanModeState()
	s.SetMode(PermissionModeAutoAccept)
	_ = s.DrainModeReminder()
	// Set same mode again — no reminder
	s.SetMode(PermissionModeAutoAccept)
	reminder := s.DrainModeReminder()
	if reminder != "" {
		t.Fatalf("DrainModeReminder() = %q, want empty (same mode)", reminder)
	}
}

func TestExitWithTargetModeSetsReminder(t *testing.T) {
	s := NewPlanModeState()
	s.SetProjectDir(t.TempDir())
	s.Enter()
	s.SetExitTargetMode(PermissionModeAutoAccept)
	s.Exit()
	reminder := s.DrainModeReminder()
	if reminder == "" {
		t.Fatal("DrainModeReminder() = empty after Exit with target mode, want non-empty")
	}
	if !strings.Contains(reminder, "auto-accept") {
		t.Fatalf("reminder = %q, want to contain 'auto-accept'", reminder)
	}
}

func TestDrainModeReminderClears(t *testing.T) {
	s := NewPlanModeState()
	s.SetMode(PermissionModeAutoAccept)
	first := s.DrainModeReminder()
	if first == "" {
		t.Fatal("first drain = empty")
	}
	second := s.DrainModeReminder()
	if second != "" {
		t.Fatalf("second drain = %q, want empty", second)
	}
}

func TestConsecutiveModeChangesOverrideReminder(t *testing.T) {
	s := NewPlanModeState()
	s.SetMode(PermissionModeAutoAccept)
	s.SetMode(PermissionModeDefault)
	reminder := s.DrainModeReminder()
	if !strings.Contains(reminder, "default") {
		t.Fatalf("reminder = %q, want to contain 'default' (last mode wins)", reminder)
	}
}

func TestExitTargetModeClearedAfterExit(t *testing.T) {
	s := NewPlanModeState()
	s.SetProjectDir(t.TempDir())
	s.Enter()
	s.SetExitTargetMode(PermissionModeAutoAccept)
	s.Exit()
	if s.Mode() != PermissionModeAutoAccept {
		t.Fatalf("mode after Exit = %q, want auto-accept", s.Mode())
	}
	// Enter plan mode again and exit without target
	// prePlanMode is auto-accept, so Exit restores to auto-accept
	s.Enter()
	s.Exit()
	if s.Mode() != PermissionModeAutoAccept {
		t.Fatalf("mode after second Exit = %q, want auto-accept (prePlanMode restored)", s.Mode())
	}
	// Start from default mode, enter plan, exit — should be default
	s.SetMode(PermissionModeDefault)
	s.Enter()
	s.Exit()
	if s.Mode() != PermissionModeDefault {
		t.Fatalf("mode after Exit from default = %q, want default", s.Mode())
	}
}

// ── Edit: Trailing Whitespace & \n Tolerance ──────────────────────────────────

func TestFindActualStringTrailingWS(t *testing.T) {
	file := "foo   \nbar  \nbaz\n"

	tests := []struct {
		name       string
		oldString  string
		wantIdx    int
		wantActual string
	}{
		{
			name:       "exact match still works",
			oldString:  "foo   \nbar  \n",
			wantIdx:    0,
			wantActual: "foo   \nbar  \n",
		},
		{
			name:       "trailing spaces stripped in old_string",
			oldString:  "foo\nbar\n",
			wantIdx:    0,
			wantActual: "foo   \nbar  \n",
		},
		{
			name:       "mixed: some lines have trailing ws, some don't",
			oldString:  "foo\nbar  \nbaz\n",
			wantIdx:    0,
			wantActual: "foo   \nbar  \nbaz\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, actual := findActualString(file, tt.oldString)
			if idx != tt.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tt.wantIdx)
			}
			if actual != tt.wantActual {
				t.Errorf("actual = %q, want %q", actual, tt.wantActual)
			}
		})
	}
}

func TestFindActualStringTrailingWSNotFound(t *testing.T) {
	file := "foo   \nbar  \n"
	// Content mismatch — not just trailing ws difference
	idx, _ := findActualString(file, "qux\n")
	if idx >= 0 {
		t.Error("expected not found, but got a match")
	}
}

func TestFindActualStringTrailingNewlineTolerance(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		oldString  string
		wantIdx    int
		wantActual string
	}{
		{
			// "foo\nbar" is a precise substring of "foo\nbar\n" — exact match wins
			name:       "old_string is exact substring (no \n tolerance needed)",
			file:       "foo\nbar\n",
			oldString:  "foo\nbar",
			wantIdx:    0,
			wantActual: "foo\nbar",
		},
		{
			// "foo\nbar\n" doesn't exist in "foo\nbar" — trim trailing \n to match
			name:       "old_string has extra trailing newline",
			file:       "foo\nbar",
			oldString:  "foo\nbar\n",
			wantIdx:    0,
			wantActual: "foo\nbar",
		},
		{
			// "hello\n" is not in file "hello world\n", but trimming \n gives "hello" which matches
			name:       "single word with trailing newline trim finds substring",
			file:       "hello world\n",
			oldString:  "hello\n",
			wantIdx:    0,
			wantActual: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, actual := findActualString(tt.file, tt.oldString)
			if idx != tt.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tt.wantIdx)
			}
			if actual != tt.wantActual {
				t.Errorf("actual = %q, want %q", actual, tt.wantActual)
			}
		})
	}
}

func TestFindActualStringTrailingNewlineUniqueness(t *testing.T) {
	// "foo" appears twice in file, "foo\n" also appears twice
	// Exact match of "foo" finds first occurrence at index 0
	file := "foo\nfoo\n"
	idx, actual := findActualString(file, "foo")
	if idx < 0 {
		t.Fatal("expected match")
	}
	if actual != "foo" {
		t.Errorf("actual = %q, want %q", actual, "foo")
	}
}

func TestEditTrailingWSReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo   \nbar  \nbaz\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "foo\nbar", // no trailing ws, no trailing \n
		NewString: "hello\nworld",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// The actualOld is "foo   \nbar  " (trailing ws preserved from file)
	// Replacing with "hello\nworld" gives: "hello\nworld\nbaz\n"
	if string(data) != "hello\nworld\nbaz\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestEditTrailingNewlineReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo\nbar\n"), 0o644)

	var e editTool
	input, _ := json.Marshal(editParams{
		Path:      path,
		OldString: "foo\nbar", // missing trailing \n — but "foo\nbar" is a precise substring
		NewString: "baz\nqux",
	})
	result := e.Run(context.Background(), input, nil)
	if result.IsError {
		t.Fatalf("IsError = true, content = %q", result.Content)
	}

	data, _ := os.ReadFile(path)
	// "foo\nbar" matches precisely, replaced with "baz\nqux"
	// The trailing \n from "foo\nbar\n" remains since it wasn't part of the match
	if string(data) != "baz\nqux\n" {
		t.Fatalf("file content = %q", string(data))
	}
}

func TestFindActualStringLastTrailingNewline(t *testing.T) {
	file := "foo\nbar\nfoo\n"
	// "foo" exact match — last occurrence at index 8
	idx, actual := findActualStringLast(file, "foo")
	if idx < 0 {
		t.Fatal("expected match")
	}
	if actual != "foo" {
		t.Errorf("actual = %q, want %q", actual, "foo")
	}
	if idx != 8 {
		t.Errorf("idx = %d, want 8", idx)
	}
}

// TestEditTrailingNewlineToleranceRealCase tests the real LLM scenario:
// old_string doesn't exist as-is, but old_string+"\n" does.
func TestEditTrailingNewlineToleranceRealCase(t *testing.T) {
	// File has "line1\nline2\n", LLM writes old_string="line1\nline2\nline3"
	// This should NOT match — it's genuinely different content
	file := "line1\nline2\n"
	idx, _ := findActualString(file, "line1\nline2\nline3")
	if idx >= 0 {
		t.Error("should not match different content")
	}
}

func TestEditTrailingNewlineTrimFromOld(t *testing.T) {
	// LLM writes old_string="hello\n" but file only has "hello" (no trailing newline)
	file := "hello"
	idx, actual := findActualString(file, "hello\n")
	if idx < 0 {
		t.Fatal("expected match via \\n tolerance")
	}
	if actual != "hello" {
		t.Errorf("actual = %q, want %q", actual, "hello")
	}
}

func TestFindActualStringTabToSpaces(t *testing.T) {
	// File has tabs, old_string uses 4 spaces (LLM rendering)
	file := "\tfunc main() {\n\t\tfmt.Println(\"hello\")\n\t}\n"

	// LLM sends spaces instead of tabs — spacesToTabs candidate should match
	idx, actual := findActualString(file, "    func main() {\n        fmt.Println(\"hello\")\n    }\n")
	if idx < 0 {
		t.Fatal("expected match via spaces→tab candidate")
	}
	// The matched actual should be the file content (with tabs)
	if actual != file {
		t.Errorf("actual = %q, want %q", actual, file)
	}
}

func TestFindActualStringSpacesToTabs(t *testing.T) {
	// File has 4 spaces, old_string uses tabs
	file := "    func main() {\n        fmt.Println(\"hello\")\n    }\n"

	// LLM sends tabs instead of spaces
	idx, actual := findActualString(file, "\tfunc main() {\n\t\tfmt.Println(\"hello\")\n\t}\n")
	if idx < 0 {
		t.Fatal("expected match — tab variant should match spaces in file")
	}
	// The candidate is the tab→spaces variant, which matches the file
	if actual != file {
		t.Errorf("actual = %q, want %q", actual, file)
	}
}

func TestFindActualStringCRLFVariant(t *testing.T) {
	// File uses LF, old_string uses CRLF
	file := "line1\nline2\nline3\n"
	idx, actual := findActualString(file, "line1\r\nline2\r\nline3\r\n")
	if idx < 0 {
		t.Fatal("expected match via CRLF→LF candidate")
	}
	if actual != file {
		t.Errorf("actual = %q, want %q", actual, file)
	}
}

func TestFindActualStringQuoteVariant(t *testing.T) {
	// File has curly quotes, old_string has straight quotes
	file := "\u201Chello\u201D"
	idx, actual := findActualString(file, `"hello"`)
	if idx < 0 {
		t.Fatal("expected match via quote normalization candidate")
	}
	if actual != file {
		t.Errorf("actual = %q, want %q", actual, file)
	}
}

func TestGenerateCandidates(t *testing.T) {
	candidates := generateCandidates("hello\tworld\r\n")
	// Should contain: tab→spaces variant, CRLF→LF variant
	foundTab := false
	foundCRLF := false
	for _, c := range candidates {
		if c == "hello    world\r\n" {
			foundTab = true
		}
		if c == "hello\tworld\n" {
			foundCRLF = true
		}
	}
	if !foundTab {
		t.Error("expected tab→spaces candidate")
	}
	if !foundCRLF {
		t.Error("expected CRLF→LF candidate")
	}
}

func TestLineStartByte(t *testing.T) {
	s := "line0\nline1\nline2\n"
	if got := lineStartByte(s, 0); got != 0 {
		t.Errorf("line 0 = %d, want 0", got)
	}
	if got := lineStartByte(s, 1); got != 6 {
		t.Errorf("line 1 = %d, want 6", got)
	}
	if got := lineStartByte(s, 2); got != 12 {
		t.Errorf("line 2 = %d, want 12", got)
	}
	if got := lineStartByte(s, 3); got != 18 {
		t.Errorf("line 3 = %d, want 18", got)
	}
}
