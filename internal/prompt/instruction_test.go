package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstructionLoaderLoad(t *testing.T) {
	// 创建临时目录模拟项目根目录
	tmpDir := t.TempDir()
	loader := NewInstructionLoader(tmpDir)

	t.Run("file not exists returns empty string", func(t *testing.T) {
		content, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if content != "" {
			t.Errorf("Load() = %q, want empty string when file does not exist", content)
		}
	})

	t.Run("file exists returns content", func(t *testing.T) {
		want := "- always use Chinese\n- write tests first"
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte(want), 0644)
		if err != nil {
			t.Fatalf("failed to create test CLAUDE.md: %v", err)
		}

		content, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if content != want {
			t.Errorf("Load() = %q, want %q", content, want)
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		raw := "\n\n  hello world  \n\n"
		err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte(raw), 0644)
		if err != nil {
			t.Fatalf("failed to create test CLAUDE.md: %v", err)
		}

		content, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if content != "hello world" {
			t.Errorf("Load() = %q, want trimmed content", content)
		}
	})

	t.Run("directory is file returns error", func(t *testing.T) {
		// 用一个文件路径作为 repoRoot，让 CLAUDE.md 路径指向一个非法位置
		fileAsDir := filepath.Join(tmpDir, "not_a_dir")
		os.WriteFile(fileAsDir, []byte("x"), 0644)
		badLoader := NewInstructionLoader(fileAsDir)

		_, err := badLoader.Load()
		if err == nil {
			t.Error("Load() should return error when repoRoot is not a directory")
		}
	})
}
