package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalkDirWithLimitKeepsRootDirsBeforeDeepFiles(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "aaa")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(deep, string(rune('a'+i))+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "dbatman"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries := walkDirWithLimit(root, 2)
	if !containsFileEntry(entries, "dbatman/") {
		t.Fatalf("entries = %v, want dbatman/ before deep files exhaust limit", entries)
	}
}

func TestWalkDirWithLimitSkipsConfiguredDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries := walkDirWithLimit(root, 10)
	if containsFileEntry(entries, ".git/") {
		t.Fatalf("entries = %v, want .git skipped", entries)
	}
	if !containsFileEntry(entries, "src/") {
		t.Fatalf("entries = %v, want src/", entries)
	}
}

func containsFileEntry(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
