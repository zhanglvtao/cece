package ui

import (
	"os"
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"
)

const walkerMaxFiles = 5000

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".svn":         true,
	".hg":          true,
	".next":        true,
	".nuxt":        true,
	".cache":       true,
}

// filesLoadedMsg is sent when the file walker finishes scanning.
type filesLoadedMsg struct {
	key  string // "project" or the expanded abs path
	root string // the absolute root that was walked
}

// FileWalker scans directories and caches file lists per root.
type FileWalker struct {
	projectDir string
	cache      map[string][]string // abs root -> relative paths
	loaded     map[string]bool
	mu         sync.RWMutex
}

// NewFileWalker creates a new FileWalker for the given project directory.
func NewFileWalker(projectDir string) *FileWalker {
	return &FileWalker{
		projectDir: projectDir,
		cache:      make(map[string][]string),
		loaded:     make(map[string]bool),
	}
}

// Load starts an async walk of an arbitrary directory.
// It invalidates any previous cache for the root before scanning.
func (w *FileWalker) Load(absRoot, key string) tea.Cmd {
	w.mu.Lock()
	w.loaded[absRoot] = false
	w.mu.Unlock()

	return func() tea.Msg {
		files := walkDir(absRoot)
		w.mu.Lock()
		w.cache[absRoot] = files
		w.loaded[absRoot] = true
		w.mu.Unlock()
		return filesLoadedMsg{key: key, root: absRoot}
	}
}

// Files returns the cached file list for the given root.
func (w *FileWalker) Files(absRoot string) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cache[absRoot]
}

// Loaded returns whether the file list for the given root has been loaded.
func (w *FileWalker) Loaded(absRoot string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.loaded[absRoot]
}

func walkDir(root string) []string {
	var entries []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		relStr := filepath.ToSlash(rel)
		if relStr == "." {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			entries = append(entries, relStr+"/")
			if len(entries) >= walkerMaxFiles {
				return filepath.SkipDir
			}
			return nil
		}
		entries = append(entries, relStr)
		if len(entries) >= walkerMaxFiles {
			return filepath.SkipDir
		}
		return nil
	})
	return entries
}
