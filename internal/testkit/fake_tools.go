package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/zhanglvtao/cece/internal/tool"
)

// CommandResult is what NewFakeBash returns for a recognised command.
type CommandResult struct {
	Stdout   string
	IsError  bool
	ExitCode int
}

// ── Bash ───────────────────────────────────────────────────────────────────

// NewFakeBash returns a tool replacement that looks up commands in
// scripts and returns the canned CommandResult, defaulting to
// "command not configured: <cmd>" when missing. It also records every
// invocation for later inspection.
func NewFakeBash(scripts map[string]CommandResult) *FakeBashTool {
	return &FakeBashTool{scripts: scripts}
}

// FakeBashTool stands in for tool.NewBash() in tests.
type FakeBashTool struct {
	mu      sync.Mutex
	scripts map[string]CommandResult
	calls   []string
}

// Calls returns the commands that have been requested so far.
func (f *FakeBashTool) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// Info satisfies tool.Tool.
func (f *FakeBashTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Bash",
		Description: "fake Bash for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"timeout": map[string]any{"type": "integer"},
			},
			"required": []string{"command"},
		},
	}
}

// Effect declares this tool as exec-effect.
func (f *FakeBashTool) Effect() tool.Effect { return tool.EffectExec }

// Run resolves command from scripts.
func (f *FakeBashTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &p)

	f.mu.Lock()
	f.calls = append(f.calls, p.Command)
	r, ok := f.scripts[p.Command]
	f.mu.Unlock()

	if !ok {
		return tool.Result{Content: fmt.Sprintf("fake bash: command not configured: %s", p.Command), IsError: true}
	}
	return tool.Result{Content: r.Stdout, IsError: r.IsError}
}

// ── In-memory FS shared by Read/Write/Edit/Glob/Grep ──────────────────────

// FakeFS is a thread-safe map[path]content. Tests construct it once and
// inject into the Read/Write/Edit/Glob/Grep fakes via NewFakeFS.
type FakeFS struct {
	mu    sync.Mutex
	files map[string]string
}

// NewFakeFS creates a FakeFS pre-populated with files.
func NewFakeFS(files map[string]string) *FakeFS {
	cp := make(map[string]string, len(files))
	for k, v := range files {
		cp[k] = v
	}
	return &FakeFS{files: cp}
}

// Read returns the content of path, or "" + false.
func (f *FakeFS) Read(p string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.files[p]
	return v, ok
}

// Write upserts a file.
func (f *FakeFS) Write(p, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[p] = content
}

// All returns a snapshot of every file path → content.
func (f *FakeFS) All() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]string, len(f.files))
	for k, v := range f.files {
		out[k] = v
	}
	return out
}

// ── Read ───────────────────────────────────────────────────────────────────

type fakeReadTool struct{ fs *FakeFS }

// NewFakeRead replaces tool.NewRead with one that reads from FakeFS.
func NewFakeRead(fs *FakeFS) tool.Tool { return &fakeReadTool{fs: fs} }

func (t *fakeReadTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Read",
		Description: "fake Read for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
			},
			"required": []string{"file_path"},
		},
	}
}

func (t *fakeReadTool) Effect() tool.Effect { return tool.EffectRead }

func (t *fakeReadTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(input, &p)
	if v, ok := t.fs.Read(p.FilePath); ok {
		return tool.Result{Content: v}
	}
	return tool.Result{Content: fmt.Sprintf("fake read: %s not found", p.FilePath), IsError: true}
}

// ── Write ──────────────────────────────────────────────────────────────────

type fakeWriteTool struct{ fs *FakeFS }

// NewFakeWrite replaces tool.NewWrite with one that writes to FakeFS.
func NewFakeWrite(fs *FakeFS) tool.Tool { return &fakeWriteTool{fs: fs} }

func (t *fakeWriteTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Write",
		Description: "fake Write for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"content":   map[string]any{"type": "string"},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

func (t *fakeWriteTool) Effect() tool.Effect { return tool.EffectWrite }

func (t *fakeWriteTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	_ = json.Unmarshal(input, &p)
	t.fs.Write(p.FilePath, p.Content)
	return tool.Result{Content: fmt.Sprintf("fake write: %d bytes → %s", len(p.Content), p.FilePath)}
}

// ── Edit ───────────────────────────────────────────────────────────────────

type fakeEditTool struct{ fs *FakeFS }

// NewFakeEdit replaces tool.NewEdit with a string-replace fake.
func NewFakeEdit(fs *FakeFS) tool.Tool { return &fakeEditTool{fs: fs} }

func (t *fakeEditTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Edit",
		Description: "fake Edit for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":   map[string]any{"type": "string"},
				"old_string":  map[string]any{"type": "string"},
				"new_string":  map[string]any{"type": "string"},
				"replace_all": map[string]any{"type": "boolean"},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}
}

func (t *fakeEditTool) Effect() tool.Effect { return tool.EffectWrite }

func (t *fakeEditTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	_ = json.Unmarshal(input, &p)
	cur, ok := t.fs.Read(p.FilePath)
	if !ok {
		return tool.Result{Content: fmt.Sprintf("fake edit: %s not found", p.FilePath), IsError: true}
	}
	if !strings.Contains(cur, p.OldString) {
		return tool.Result{Content: fmt.Sprintf("fake edit: old_string not found in %s", p.FilePath), IsError: true}
	}
	var updated string
	if p.ReplaceAll {
		updated = strings.ReplaceAll(cur, p.OldString, p.NewString)
	} else {
		updated = strings.Replace(cur, p.OldString, p.NewString, 1)
	}
	t.fs.Write(p.FilePath, updated)
	return tool.Result{Content: fmt.Sprintf("fake edit: %s updated", p.FilePath)}
}

// ── Glob ───────────────────────────────────────────────────────────────────

type fakeGlobTool struct{ fs *FakeFS }

// NewFakeGlob replaces tool.NewGlob with a path-prefix matcher.
func NewFakeGlob(fs *FakeFS) tool.Tool { return &fakeGlobTool{fs: fs} }

func (t *fakeGlobTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Glob",
		Description: "fake Glob for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *fakeGlobTool) Effect() tool.Effect { return tool.EffectRead }

func (t *fakeGlobTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	_ = json.Unmarshal(input, &p)
	var matches []string
	for f := range t.fs.All() {
		if matchGlob(p.Pattern, f) {
			matches = append(matches, f)
		}
	}
	return tool.Result{Content: strings.Join(matches, "\n")}
}

// matchGlob implements a minimal glob: "*" matches any sequence except "/".
func matchGlob(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	matched, err := path.Match(pattern, name)
	if err == nil && matched {
		return true
	}
	// Also match by basename.
	if matched, err := path.Match(pattern, path.Base(name)); err == nil && matched {
		return true
	}
	// Substring fallback for "**" patterns.
	if strings.Contains(pattern, "**") {
		core := strings.Trim(strings.ReplaceAll(pattern, "**", ""), "/*")
		if core == "" {
			return true
		}
		return strings.Contains(name, core)
	}
	return false
}

// ── Grep ───────────────────────────────────────────────────────────────────

type fakeGrepTool struct{ fs *FakeFS }

// NewFakeGrep replaces tool.NewGrep with a substring matcher.
func NewFakeGrep(fs *FakeFS) tool.Tool { return &fakeGrepTool{fs: fs} }

func (t *fakeGrepTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Grep",
		Description: "fake Grep for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *fakeGrepTool) Effect() tool.Effect { return tool.EffectRead }

func (t *fakeGrepTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal(input, &p)
	var lines []string
	for fp, content := range t.fs.All() {
		for i, ln := range strings.Split(content, "\n") {
			if strings.Contains(ln, p.Pattern) {
				lines = append(lines, fmt.Sprintf("%s:%d:%s", fp, i+1, ln))
			}
		}
	}
	return tool.Result{Content: strings.Join(lines, "\n")}
}

// ── WebFetch ───────────────────────────────────────────────────────────────

type fakeWebFetchTool struct{ responses map[string]string }

// NewFakeWebFetch returns canned bodies for known URLs.
func NewFakeWebFetch(responses map[string]string) tool.Tool {
	return &fakeWebFetchTool{responses: responses}
}

func (t *fakeWebFetchTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "WebFetch",
		Description: "fake WebFetch for testkit",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"uri": map[string]any{"type": "string"},
			},
			"required": []string{"uri"},
		},
	}
}

func (t *fakeWebFetchTool) Effect() tool.Effect { return tool.EffectRead }

func (t *fakeWebFetchTool) Run(_ context.Context, input json.RawMessage, _ tool.Emitter) tool.Result {
	var p struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(input, &p)
	if v, ok := t.responses[p.URI]; ok {
		return tool.Result{Content: v}
	}
	return tool.Result{Content: fmt.Sprintf("fake webfetch: no response for %s", p.URI), IsError: true}
}

// ── MCP ────────────────────────────────────────────────────────────────────

// NewFakeMCPTool creates a fake MCP tool with the given name (should start
// with "mcp_" prefix) and canned result.
func NewFakeMCPTool(name, result string) tool.Tool {
	return &fakeMCPTool{name: name, result: result}
}

type fakeMCPTool struct {
	name   string
	result string
}

func (f *fakeMCPTool) Info() tool.Definition {
	return tool.Definition{
		Name:        f.name,
		Description: "fake MCP tool for testing",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
		},
	}
}

func (f *fakeMCPTool) Effect() tool.Effect { return tool.EffectRead }

func (f *fakeMCPTool) Run(_ context.Context, _ json.RawMessage, _ tool.Emitter) tool.Result {
	return tool.Result{Content: f.result}
}
