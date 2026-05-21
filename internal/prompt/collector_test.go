package prompt

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cece/internal/tool"
)

// stubToolListProvider returns a fixed set of tool definitions for testing.
type stubToolListProvider struct {
	defs []tool.Definition
}

func (s stubToolListProvider) Definitions() []tool.Definition {
	return s.defs
}

func TestDefaultSessionCollectorCollectsEnvInfo(t *testing.T) {
	dir := t.TempDir()
	provider := stubToolListProvider{
		defs: []tool.Definition{
			{Name: "Bash", Description: "run commands"},
		},
	}

	collector := NewDefaultSessionCollector(dir, provider)
	ctx := context.Background()

	sc, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if sc.RepoRoot != dir {
		t.Fatalf("RepoRoot = %q, want %q", sc.RepoRoot, dir)
	}
	if sc.OSName == "" {
		t.Fatal("OSName should not be empty")
	}
	if sc.ToolDescriptions == "" {
		t.Fatal("ToolDescriptions should not be empty when tools are provided")
	}
	if !contains(sc.ToolDescriptions, "Bash") {
		t.Fatalf("ToolDescriptions missing Bash: %q", sc.ToolDescriptions)
	}
}

func TestDefaultSessionCollectorLoadsCLAUDEmd(t *testing.T) {
	dir := t.TempDir()
	content := "always use Chinese\nformat with go fmt"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	collector := NewDefaultSessionCollector(dir, stubToolListProvider{})
	sc, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if sc.CLAUDEmd != content {
		t.Fatalf("CLAUDEmd = %q, want %q", sc.CLAUDEmd, content)
	}
}

func TestDefaultSessionCollectorNoCLAUDEmd(t *testing.T) {
	dir := t.TempDir()
	collector := NewDefaultSessionCollector(dir, stubToolListProvider{})
	sc, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if sc.CLAUDEmd != "" {
		t.Fatalf("CLAUDEmd should be empty when file missing, got %q", sc.CLAUDEmd)
	}
}

func TestDefaultSessionCollectorDetectsGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Not a git repo — IsGitRepo should be false
	collector := NewDefaultSessionCollector(dir, stubToolListProvider{})
	sc, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if sc.IsGitRepo {
		t.Fatal("IsGitRepo should be false for temp dir without .git")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
