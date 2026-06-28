package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/tool"
)

type staticTestTool struct{}

func (staticTestTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Static",
		Description: "static test tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (staticTestTool) Run(ctx context.Context, input json.RawMessage, emitter tool.Emitter) tool.Result {
	if emitter != nil {
		emitter.Emit("progress\n")
	}
	return tool.Result{Content: "ok"}
}

type truncatedTestTool struct{}

func (truncatedTestTool) Info() tool.Definition {
	return tool.Definition{
		Name:        "Truncated",
		Description: "truncated test tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (truncatedTestTool) Run(ctx context.Context, input json.RawMessage, emitter tool.Emitter) tool.Result {
	return tool.Result{
		Content:       "preview",
		Truncated:     true,
		OutputPath:    ".cece/tool-results/truncated.txt",
		OriginalBytes: 9000,
		PreviewBytes:  2000,
	}
}

func TestToolExecutorRecordsClosureEvidence(t *testing.T) {
	registry := tool.NewRegistry(staticTestTool{})
	var evidence []ClosureEvidence
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil, func(ev ClosureEvidence) {
		evidence = append(evidence, ev)
	})

	blocks := executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
		ID:    "call-test",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"go test ./internal/agent"}`),
	}}, nil)

	if len(evidence) != 1 {
		t.Fatalf("evidence len = %d, want 1", len(evidence))
	}
	if evidence[0].ToolUseID != "call-test" || evidence[0].Kind != ClosureEvidenceVerification || evidence[0].Command != "go test ./internal/agent" {
		t.Fatalf("evidence = %+v", evidence[0])
	}
	if len(blocks) != 1 || blocks[0].ToolResult == nil || !strings.Contains(blocks[0].ToolResult.Content, "ClosureEvidence: tool_result=call-test kind=verification") {
		t.Fatalf("tool result content = %+v", blocks)
	}
}

func TestToolExecutorRecordsDjangoRunnerAsVerificationEvidence(t *testing.T) {
	registry := tool.NewRegistry(staticTestTool{})
	var evidence []ClosureEvidence
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil, func(ev ClosureEvidence) {
		evidence = append(evidence, ev)
	})

	blocks := executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
		ID:    "call-django-test",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"python tests/runtests.py annotations.tests.NonAggregateAnnotationTestCase.test_order_by_multiline_rawsql"}`),
	}}, nil)

	if len(evidence) != 1 {
		t.Fatalf("evidence len = %d, want 1", len(evidence))
	}
	if evidence[0].Kind != ClosureEvidenceVerification || evidence[0].ToolUseID != "call-django-test" {
		t.Fatalf("evidence = %+v", evidence[0])
	}
	if len(blocks) != 1 || blocks[0].ToolResult == nil {
		t.Fatalf("blocks = %+v", blocks)
	}
	content := blocks[0].ToolResult.Content
	if !strings.Contains(content, "ClosureEvidence: tool_result=call-django-test kind=verification") {
		t.Fatalf("tool result content = %q, want closure evidence line", content)
	}
	if !strings.Contains(content, "verification_tool_result_refs=[\"call-django-test\"]") {
		t.Fatalf("tool result content = %q, want copy-paste verification refs hint", content)
	}
}

func TestIsVerificationCommandRecognizesPythonTestEntrypoints(t *testing.T) {
	commands := []string{
		"python -m pytest tests/test_example.py",
		"python tests/runtests.py model_fields.test_filepathfield",
		"./tests/runtests.py --verbosity 2 --settings=test_sqlite --parallel 1 test_utils.tests",
	}
	for _, cmd := range commands {
		if !isVerificationCommand(cmd) {
			t.Fatalf("isVerificationCommand(%q) = false, want true", cmd)
		}
	}
}

func TestToolExecutorPropagatesTruncatedMetadata(t *testing.T) {
	registry := tool.NewRegistry(truncatedTestTool{})
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)
	blocks := executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
		ID:    "tool-1",
		Name:  "Truncated",
		Input: json.RawMessage(`{}`),
	}}, nil)
	if len(blocks) != 1 || blocks[0].ToolResult == nil {
		t.Fatalf("blocks = %#v, want one tool result", blocks)
	}
	tr := blocks[0].ToolResult
	if !tr.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if tr.OutputPath != ".cece/tool-results/truncated.txt" || tr.OriginalBytes != 9000 || tr.PreviewBytes != 2000 {
		t.Fatalf("artifact metadata = path %q original %d preview %d, want propagated", tr.OutputPath, tr.OriginalBytes, tr.PreviewBytes)
	}
}

func TestToolExecutorExecuteBatchAllowsNilEventChannel(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(staticTestTool{})
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)

	done := make(chan []ApiContentBlock, 1)
	go func() {
		done <- executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
			ID:    "tool-1",
			Name:  "Static",
			Input: json.RawMessage(`{}`),
		}}, nil)
	}()

	select {
	case blocks := <-done:
		if len(blocks) != 1 {
			t.Fatalf("len(blocks) = %d, want 1", len(blocks))
		}
		if blocks[0].ToolResult == nil || blocks[0].ToolResult.Content != "ok" {
			t.Fatalf("tool result = %#v, want ok", blocks[0].ToolResult)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecuteBatch blocked with nil event channel")
	}
}

func TestToolExecutorAllowsPlanModeMockupContentWrites(t *testing.T) {
	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	registry := tool.NewRegistry(tool.NewWrite())
	executor := NewToolExecutor(registry, planState, nil, ToolResultPolicy{}, nil)
	mockupPath := filepath.Join(projectDir, ".superpowers", "brainstorm", "session-1", "content", "mockup.html")
	input, _ := json.Marshal(map[string]string{"path": mockupPath, "content": "<html>mockup</html>"})

	blocks := executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
		ID:    "write-1",
		Name:  "Write",
		Input: input,
	}}, nil)

	if len(blocks) != 1 || blocks[0].ToolResult == nil {
		t.Fatalf("blocks = %#v, want one tool result", blocks)
	}
	if blocks[0].ToolResult.IsError {
		t.Fatalf("Write returned error: %s", blocks[0].ToolResult.Content)
	}
	data, err := os.ReadFile(mockupPath)
	if err != nil {
		t.Fatalf("mockup was not written: %v", err)
	}
	if string(data) != "<html>mockup</html>" {
		t.Fatalf("mockup content = %q", string(data))
	}
}

func TestToolExecutorRejectsPlanModeWritesOutsideAllowlist(t *testing.T) {
	projectDir := t.TempDir()
	planState := tool.NewPlanModeState()
	planState.SetProjectDir(projectDir)
	planState.Enter()

	registry := tool.NewRegistry(tool.NewWrite())
	executor := NewToolExecutor(registry, planState, nil, ToolResultPolicy{}, nil)
	sourcePath := filepath.Join(projectDir, "internal", "x.go")
	input, _ := json.Marshal(map[string]string{"path": sourcePath, "content": "package internal"})

	blocks := executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{{
		ID:    "write-1",
		Name:  "Write",
		Input: input,
	}}, nil)

	if len(blocks) != 1 || blocks[0].ToolResult == nil {
		t.Fatalf("blocks = %#v, want one tool result", blocks)
	}
	if !blocks[0].ToolResult.IsError {
		t.Fatalf("Write succeeded outside allowlist: %s", blocks[0].ToolResult.Content)
	}
	if !strings.Contains(blocks[0].ToolResult.Content, tool.DefaultPlanModeMockupAllowPattern) {
		t.Fatalf("error = %q, want allowlist details", blocks[0].ToolResult.Content)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("source write should be blocked, stat err = %v", err)
	}
}

type blockingTestTool struct {
	name    string
	blocker *toolRunBlocker
}

func (t blockingTestTool) Info() tool.Definition {
	return tool.Definition{
		Name:        t.name,
		Description: "blocking test tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t blockingTestTool) Run(ctx context.Context, input json.RawMessage, emitter tool.Emitter) tool.Result {
	return t.blocker.run(t.name)
}

type toolRunBlocker struct {
	mu      sync.Mutex
	started []string
	release chan struct{}
}

func newToolRunBlocker() *toolRunBlocker {
	return &toolRunBlocker{release: make(chan struct{})}
}

func (b *toolRunBlocker) run(name string) tool.Result {
	b.mu.Lock()
	b.started = append(b.started, name)
	b.mu.Unlock()
	<-b.release
	return tool.Result{Content: name}
}

func (b *toolRunBlocker) startedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.started)
}

func (b *toolRunBlocker) startedNames() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.started...)
}

func waitForStarted(t *testing.T, b *toolRunBlocker, n int) {
	t.Helper()
	deadline := time.After(200 * time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if b.startedCount() >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("started = %v, want at least %d", b.startedNames(), n)
		case <-ticker.C:
		}
	}
}

func assertNoMoreStarted(t *testing.T, b *toolRunBlocker, n int) {
	t.Helper()
	time.Sleep(30 * time.Millisecond)
	if got := b.startedCount(); got != n {
		t.Fatalf("started = %v, want exactly %d", b.startedNames(), n)
	}
}

func registerBlockingTools(registry *tool.Registry, blocker *toolRunBlocker, names ...string) {
	for _, name := range names {
		registry.Register(blockingTestTool{name: name, blocker: blocker})
	}
}

func TestToolExecutorOnlyRunsSafeToolsConcurrently(t *testing.T) {
	blocker := newToolRunBlocker()
	registry := tool.NewRegistry()
	registerBlockingTools(registry, blocker, "Read", "Grep", "Bash", "Glob", "WebFetch")
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)

	done := make(chan []ApiContentBlock, 1)
	go func() {
		done <- executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{
			{ID: "1", Name: "Read", Input: json.RawMessage(`{}`)},
			{ID: "2", Name: "Grep", Input: json.RawMessage(`{}`)},
			{ID: "3", Name: "Bash", Input: json.RawMessage(`{}`)},
			{ID: "4", Name: "Glob", Input: json.RawMessage(`{}`)},
			{ID: "5", Name: "WebFetch", Input: json.RawMessage(`{}`)},
		}, nil)
	}()

	waitForStarted(t, blocker, 2)
	assertNoMoreStarted(t, blocker, 2)
	close(blocker.release)
	waitForStarted(t, blocker, 5)

	select {
	case blocks := <-done:
		if len(blocks) != 5 {
			t.Fatalf("len(blocks) = %d, want 5", len(blocks))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecuteBatch did not finish")
	}
}

func TestToolExecutorRunsAgentStartConcurrently(t *testing.T) {
	blocker := newToolRunBlocker()
	registry := tool.NewRegistry()
	registerBlockingTools(registry, blocker, tool.AgentToolName)
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)

	done := make(chan []ApiContentBlock, 1)
	go func() {
		done <- executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{
			{ID: "1", Name: tool.AgentToolName, Input: json.RawMessage(`{"operation":"start"}`)},
			{ID: "2", Name: tool.AgentToolName, Input: json.RawMessage(`{}`)},
		}, nil)
	}()

	waitForStarted(t, blocker, 2)
	close(blocker.release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecuteBatch did not finish")
	}
}

func TestToolExecutorDoesNotRunAgentControlOpsConcurrently(t *testing.T) {
	blocker := newToolRunBlocker()
	registry := tool.NewRegistry()
	registerBlockingTools(registry, blocker, tool.AgentToolName)
	executor := NewToolExecutor(registry, nil, nil, ToolResultPolicy{}, nil)

	done := make(chan []ApiContentBlock, 1)
	go func() {
		done <- executor.ExecuteBatch(context.Background(), []ApiToolUseBlock{
			{ID: "1", Name: tool.AgentToolName, Input: json.RawMessage(`{"operation":"status"}`)},
			{ID: "2", Name: tool.AgentToolName, Input: json.RawMessage(`{"operation":"cancel"}`)},
		}, nil)
	}()

	waitForStarted(t, blocker, 1)
	assertNoMoreStarted(t, blocker, 1)
	close(blocker.release)
	waitForStarted(t, blocker, 2)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecuteBatch did not finish")
	}
}

func Example_canRunToolConcurrently() {
	fmt.Println(canRunToolConcurrently(ApiToolUseBlock{Name: "Read", Input: json.RawMessage(`{}`)}))
	fmt.Println(canRunToolConcurrently(ApiToolUseBlock{Name: "Bash", Input: json.RawMessage(`{}`)}))
	fmt.Println(canRunToolConcurrently(ApiToolUseBlock{Name: tool.AgentToolName, Input: json.RawMessage(`{"operation":"start"}`)}))
	fmt.Println(canRunToolConcurrently(ApiToolUseBlock{Name: tool.AgentToolName, Input: json.RawMessage(`{"operation":"status"}`)}))
	// Output:
	// true
	// false
	// true
	// false
}
