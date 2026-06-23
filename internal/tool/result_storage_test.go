package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResultStoreLeavesSmallOutputInline(t *testing.T) {
	store := NewResultStore(t.TempDir())
	result := store.Apply("Bash", Result{Content: "hello"}, ResultStoragePolicy{MaxBytes: 10, PreviewBytes: 10})
	if result.Truncated {
		t.Fatal("Truncated = true, want false")
	}
	if result.Content != "hello" {
		t.Fatalf("Content = %q, want hello", result.Content)
	}
	if result.OutputPath != "" {
		t.Fatalf("OutputPath = %q, want empty", result.OutputPath)
	}
}

func TestResultStorePersistsLargeOutput(t *testing.T) {
	store := NewResultStore(t.TempDir())
	content := strings.Repeat("a", 12) + "TAIL"
	result := store.Apply("Bash", Result{Content: content}, ResultStoragePolicy{MaxBytes: 10, PreviewBytes: 10})
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
	if string(stored) != content {
		t.Fatalf("stored content = %q, want original", string(stored))
	}
	if !strings.Contains(result.Content, "Full output saved to:") || !strings.Contains(result.Content, result.OutputPath) {
		t.Fatalf("Content = %q, want saved output hint", result.Content)
	}
	if strings.Contains(result.Content, "TAIL") {
		t.Fatalf("Content = %q, should only contain preview", result.Content)
	}
	if result.OriginalBytes != len(content) || result.PreviewBytes != 10 {
		t.Fatalf("metadata = original %d preview %d, want %d/10", result.OriginalBytes, result.PreviewBytes, len(content))
	}
}

func TestResultStoreFallsBackWhenWriteFails(t *testing.T) {
	projectFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(projectFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewResultStore(projectFile)
	content := strings.Repeat("a", maxOutputLen+1)
	result := store.Apply("Bash", Result{Content: content}, ResultStoragePolicy{MaxBytes: 10, PreviewBytes: 10})
	if !result.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if result.OutputPath != "" {
		t.Fatalf("OutputPath = %q, want empty fallback path", result.OutputPath)
	}
	if !strings.Contains(result.Content, "truncated") {
		t.Fatalf("Content = %q, want fallback truncation marker", result.Content)
	}
}
