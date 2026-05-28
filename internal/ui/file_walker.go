package ui

import (
	"os"
	"path/filepath"
	"strings"
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

// LoadProject starts an async walk of the project directory.
func (w *FileWalker) LoadProject() tea.Cmd {
	return w.Load(w.projectDir, "project")
}

// Load starts an async walk of an arbitrary directory.
func (w *FileWalker) Load(absRoot, key string) tea.Cmd {
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
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			// Skip hidden dirs
			if len(base) > 1 && base[0] == '.' {
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
