package ui

import (
	"path/filepath"
	"strings"

	"cece/internal/ui/picker"
	tea "charm.land/bubbletea/v2"
)

const filePopupMaxHeight = 10

// fileEntry is a single file candidate.
type fileEntry struct {
	path string
}

// FilePopup wraps a compact Picker for @ file completion.
type FilePopup struct {
	walker  *FileWalker
	picker  *picker.Picker
	entries []fileEntry
	open    bool
	spec    atSpec
}

// NewFilePopup creates a new FilePopup component.
func NewFilePopup(projectDir string) *FilePopup {
	return &FilePopup{
		walker: NewFileWalker(projectDir),
	}
}

// Open shows the popup. Returns a tea.Cmd to trigger async file loading if needed.
func (p *FilePopup) Open(spec atSpec) tea.Cmd {
	p.open = true
	p.spec = spec
	var cmd tea.Cmd
	if !p.walker.Loaded() {
		cmd = p.walker.Load()
	}
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

// SelectedFile returns the currently selected file path.
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
	return e.path, true
}

// UpdateFilter sets the filter query and rebuilds the filtered list.
func (p *FilePopup) UpdateFilter(spec atSpec) {
	if p.picker == nil {
		return
	}
	p.spec = spec
	p.rebuildEntries()
	items := make([]any, len(p.entries))
	for i, e := range p.entries {
		items[i] = e
	}
	p.picker.SetItems(items)
	p.picker.SetFilter(spec.FileName)
}

// OnFilesLoaded is called when the async file walk completes.
func (p *FilePopup) OnFilesLoaded() {
	if p.open {
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
		return picker.FormatItem(e.path, selected)
	})
	pk.SetCompact(true)
	pk.SetFilterFn(func(item any, q string) bool {
		e := item.(fileEntry)
		return fileMatches(e, p.spec.BaseDir, q)
	})
	p.picker = pk
}

// rebuildEntries rebuilds the entry list from the walker cache,
// filtered by the current spec's BaseDir.
func (p *FilePopup) rebuildEntries() {
	files := p.walker.Files()
	baseDir := p.spec.BaseDir
	var entries []fileEntry
	for _, f := range files {
		if baseDir != "" && !strings.HasPrefix(f, baseDir) {
			continue
		}
		entries = append(entries, fileEntry{path: f})
	}
	p.entries = entries
}

// fileMatches checks if a fileEntry matches the baseDir prefix and fileName filter.
func fileMatches(e fileEntry, baseDir, fileNameQuery string) bool {
	if baseDir != "" && !strings.HasPrefix(e.path, baseDir) {
		return false
	}
	if fileNameQuery == "" {
		return true
	}
	// Match against the filename (last segment) using case-insensitive substring
	base := filepath.Base(e.path)
	return containsFold(base, fileNameQuery)
}

// View renders the popup as plain lines.
func (p *FilePopup) View(_ int) string {
	if !p.open || p.picker == nil {
		return ""
	}
	if !p.walker.Loaded() {
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
	if !p.walker.Loaded() {
		return 1
	}
	if p.picker == nil {
		return 0
	}
	return p.picker.Height()
}
