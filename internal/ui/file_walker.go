package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

const (
	walkerMaxDepth = 8
	walkerMaxFiles = 2000
)

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".svn":         true,
	".hg":          true,
	"dist":         true,
	"build":        true,
	"out":          true,
	".next":        true,
	".nuxt":        true,
	"target":       true,
	".cache":       true,
}

// filesLoadedMsg is sent when the file walker finishes scanning.
type filesLoadedMsg struct{ files []string }

// FileWalker scans a project directory and caches the file list.
type FileWalker struct {
	projectDir string
	files      []string
	loaded     bool
	mu         sync.RWMutex
}

// NewFileWalker creates a new FileWalker for the given project directory.
func NewFileWalker(projectDir string) *FileWalker {
	return &FileWalker{projectDir: projectDir}
}

// Load starts an async walk of the project directory.
func (w *FileWalker) Load() tea.Cmd {
	return func() tea.Msg {
		files := walkDir(w.projectDir)
		w.mu.Lock()
		w.files = files
		w.loaded = true
		w.mu.Unlock()
		return filesLoadedMsg{files: files}
	}
}

// Files returns the cached file list (relative paths).
func (w *FileWalker) Files() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.files
}

// Loaded returns whether the file list has been loaded.
func (w *FileWalker) Loaded() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.loaded
}

func walkDir(root string) []string {
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			rel, _ := filepath.Rel(root, path)
			relStr := filepath.ToSlash(rel)
			base := filepath.Base(relStr)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			// Skip hidden dirs
			if len(base) > 1 && base[0] == '.' {
				return filepath.SkipDir
			}
			// Depth limit
			depth := strings.Count(relStr, "/")
			if depth >= walkerMaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		relStr := filepath.ToSlash(rel)
		// Skip hidden files
		if strings.HasPrefix(filepath.Base(relStr), ".") {
			return nil
		}
		files = append(files, relStr)
		if len(files) >= walkerMaxFiles {
			return filepath.SkipDir
		}
		return nil
	})
	return files
}
