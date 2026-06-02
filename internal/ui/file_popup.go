package ui

import (
	"path/filepath"
	"sort"
	"strings"

	"cece/internal/ui/picker"
	tea "charm.land/bubbletea/v2"
)

const filePopupMaxHeight = 10

// fileEntry is a single file candidate.
type fileEntry struct {
	path     string // display path
	FullPath string // insertion path
	isDir    bool   // true if this entry is a directory
}

// FilePopup wraps a compact Picker for @ file completion.
type FilePopup struct {
	walker  *FileWalker
	picker  *picker.Picker
	entries []fileEntry
	styles  Styles
	open    bool
	spec    atSpec
}

// NewFilePopup creates a new FilePopup component.
func NewFilePopup(projectDir string) *FilePopup {
	return &FilePopup{
		walker: NewFileWalker(projectDir),
		styles: DefaultStyles(),
	}
}

// Open shows the popup. Always triggers a fresh file walk so new files are visible.
func (p *FilePopup) Open(spec atSpec) tea.Cmd {
	p.open = true
	p.spec = spec
	cmd := p.walker.Load(spec.AbsRoot, spec.AbsRoot)
	p.buildPicker()
	p.picker.SetFilter(spec.FileName)
	return cmd
}

// Close hides the popup.
func (p *FilePopup) Close() {
	p.open = false
	p.picker = nil
}

// Active returns whether the popup is visible.
func (p *FilePopup) Active() bool { return p.open }

// SelectUp moves the selection up.
func (p *FilePopup) SelectUp() {
	if p.picker != nil {
		p.picker.Up()
	}
}

// SelectDown moves the selection down.
func (p *FilePopup) SelectDown() {
	if p.picker != nil {
		p.picker.Down()
	}
}

// SelectedFile returns the currently selected file path (the insertion text).
func (p *FilePopup) SelectedFile() (string, bool) {
	if p.picker == nil {
		return "", false
	}
	item := p.picker.Selected()
	if item == nil {
		return "", false
	}
	e, ok := item.(fileEntry)
	if !ok {
		return "", false
	}
	return e.FullPath, true
}

// UpdateFilter sets the filter query and rebuilds the filtered list.
func (p *FilePopup) UpdateFilter(spec atSpec) tea.Cmd {
	if p.picker == nil {
		return nil
	}
	// If root changed, need to load new directory
	if spec.AbsRoot != p.spec.AbsRoot {
		p.spec = spec
		return p.walker.Load(spec.AbsRoot, spec.AbsRoot)
	}
	p.spec = spec
	p.rebuildEntries()
	items := make([]any, len(p.entries))
	for i, e := range p.entries {
		items[i] = e
	}
	p.picker.SetItems(items)
	p.picker.SetFilter(spec.FileName)
	return nil
}

// OnFilesLoaded is called when the async file walk completes.
func (p *FilePopup) OnFilesLoaded(root string) {
	if p.open && p.spec.AbsRoot == root {
		p.buildPicker()
		p.picker.SetFilter(p.spec.FileName)
	}
}

// buildPicker creates the internal picker with the current entries.
func (p *FilePopup) buildPicker() {
	p.rebuildEntries()
	items := make([]any, len(p.entries))
	for i, e := range p.entries {
		items[i] = e
	}
	pk := picker.New("", items, filePopupMaxHeight, func(item any, selected bool) string {
		e := item.(fileEntry)
		return styledPickerItem(p.styles.Picker.Cursor, e.path, selected)
	})
	pk.SetCompact(true)
	pk.SetFilterFn(func(item any, q string) bool {
		e := item.(fileEntry)
		return fileMatches(e, q)
	})
	p.picker = pk
}

// rebuildEntries rebuilds the entry list from the walker cache,
// sorted by relevance: directories first, then non-hidden first, shallow depth first, better match first.
func (p *FilePopup) rebuildEntries() {
	files := p.walker.Files(p.spec.AbsRoot)
	entries := make([]fileEntry, 0, len(files))
	for _, f := range files {
		isDir := strings.HasSuffix(f, "/")
		var fullPath string
		if p.spec.IsAbs || p.spec.BaseDir != "" {
			fullPath = p.spec.BaseDir + f
		} else {
			fullPath = f
		}
		entries = append(entries, fileEntry{path: f, FullPath: fullPath, isDir: isDir})
	}

	query := p.spec.FileName
	sort.SliceStable(entries, func(i, j int) bool {
		return fileLess(entries[i], entries[j], query)
	})

	p.entries = entries
}

// fileLess defines the sort order for file entries.
// Priority: directory > file, prefix match > contains match > no match,
//           non-hidden > hidden, shallow > deep.
func fileLess(a, b fileEntry, query string) bool {
	// Directories before files
	if a.isDir != b.isDir {
		return a.isDir
	}

	sa := fileScore(a, query)
	sb := fileScore(b, query)

	// Match rank: prefix(2) > contains(1) > none(0)
	if sa.matchRank != sb.matchRank {
		return sa.matchRank > sb.matchRank
	}
	// Non-hidden before hidden
	if sa.hidden != sb.hidden {
		return !sa.hidden
	}
	// Shallow before deep
	if sa.depth != sb.depth {
		return sa.depth < sb.depth
	}
	// Alphabetical as tiebreaker
	return a.path < b.path
}

type fileScoreInfo struct {
	depth     int
	hidden    bool
	matchRank int // 2=prefix, 1=contains, 0=none
}

func fileScore(e fileEntry, query string) fileScoreInfo {
	base := filepath.Base(e.path)
	if e.isDir {
		base = strings.TrimSuffix(base, "/")
	}
	depth := strings.Count(strings.TrimSuffix(e.path, "/"), "/")
	hidden := isHiddenPath(e.path)

	rank := 0
	if query != "" {
		lower := strings.ToLower(base)
		q := strings.ToLower(query)
		if strings.HasPrefix(lower, q) {
			rank = 2
		} else if strings.Contains(lower, q) {
			rank = 1
		}
	}

	return fileScoreInfo{depth: depth, hidden: hidden, matchRank: rank}
}

// isHiddenPath checks if any path segment starts with "." (hidden directory/file).
func isHiddenPath(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if len(part) > 1 && part[0] == '.' {
			return true
		}
	}
	return false
}

// fileMatches checks if a fileEntry matches the fileName filter.
func fileMatches(e fileEntry, fileNameQuery string) bool {
	if fileNameQuery == "" {
		return true
	}
	base := filepath.Base(e.path)
	if e.isDir {
		base = strings.TrimSuffix(base, "/")
	}
	return containsFold(base, fileNameQuery)
}

// View renders the popup as plain lines.
func (p *FilePopup) View(_ int) string {
	if !p.open || p.picker == nil {
		return ""
	}
	if !p.walker.Loaded(p.spec.AbsRoot) {
		return "  Loading files..."
	}
	if len(p.entries) == 0 {
		return ""
	}
	return p.picker.View()
}

// Height returns the rendered height of the popup (0 if not open).
func (p *FilePopup) Height() int {
	if !p.open {
		return 0
	}
	if !p.walker.Loaded(p.spec.AbsRoot) {
		return 1
	}
	if p.picker == nil {
		return 0
	}
	return p.picker.Height()
}
