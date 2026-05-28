package ui

import (
	"cece/internal/skill"
	"cece/internal/ui/picker"
)

const slashPopupMaxHeight = 8

var builtinSlashCommands = []struct {
	cmd, desc string
}{
	{"/model", "Switch model"},
	{"/resume", "Resume a session"},
	{"/clear", "Clear conversation history"},
	{"/compact", "Compress conversation history"},
	{"/skills", "List available skills"},
	{"/mcp", "Manage MCP servers"},
	{"/tool", "List registered tools"},
}

// slashEntry is a single slash command candidate.
type slashEntry struct {
	command     string
	description string
	source      string // "builtin" | "skill"
}

// SlashPopup wraps a compact Picker for slash command completion.
type SlashPopup struct {
	picker *picker.Picker
	entries []slashEntry
	open   bool
}

// NewSlashPopup creates a new SlashPopup component.
func NewSlashPopup(_ Styles) *SlashPopup {
	return &SlashPopup{}
}

// SetSkills rebuilds the candidate list from builtin commands + skills.
func (p *SlashPopup) SetSkills(skills []*skill.Skill) {
	var entries []slashEntry
	for _, b := range builtinSlashCommands {
		entries = append(entries, slashEntry{command: b.cmd, description: b.desc, source: "builtin"})
	}
	for _, s := range skills {
		entries = append(entries, slashEntry{command: "/" + s.Name, description: s.Description, source: "skill"})
	}
	p.entries = entries
}

// buildPicker creates the internal picker with the current entries.
func (p *SlashPopup) buildPicker() {
	items := make([]any, len(p.entries))
	for i, e := range p.entries {
		items[i] = e
	}
	pk := picker.New("", items, slashPopupMaxHeight, func(item any, selected bool) string {
		e := item.(slashEntry)
		text := e.command
		if e.description != "" {
			text += "  " + e.description
		}
		return picker.FormatItem(text, selected)
	})
	pk.SetCompact(true)
	pk.SetFilterFn(func(item any, q string) bool {
		e := item.(slashEntry)
		return containsAny(e, q)
	})
	p.picker = pk
}

// containsAny does simple substring matching on a slashEntry.
func containsAny(e slashEntry, q string) bool {
	return containsFold(e.command, q) || containsFold(e.description, q)
}

func containsFold(s, substr string) bool {
	slen := len(s)
	sublen := len(substr)
	if sublen > slen {
		return false
	}
	for i := 0; i <= slen-sublen; i++ {
		if stringsEqualFold(s[i:i+sublen], substr) {
			return true
		}
	}
	return false
}

func stringsEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca == cb {
			continue
		}
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// Open shows the popup with the given query filter.
func (p *SlashPopup) Open(query string) {
	p.open = true
	p.buildPicker()
	p.picker.SetFilter(query)
}

// Close hides the popup.
func (p *SlashPopup) Close() {
	p.open = false
	p.picker = nil
}

// Active returns whether the popup is visible.
func (p *SlashPopup) Active() bool { return p.open }

// SelectUp moves the selection up.
func (p *SlashPopup) SelectUp() {
	if p.picker != nil {
		p.picker.Up()
	}
}

// SelectDown moves the selection down.
func (p *SlashPopup) SelectDown() {
	if p.picker != nil {
		p.picker.Down()
	}
}

// SelectedCommand returns the currently selected command string.
func (p *SlashPopup) SelectedCommand() (string, bool) {
	if p.picker == nil {
		return "", false
	}
	item := p.picker.Selected()
	if item == nil {
		return "", false
	}
	e, ok := item.(slashEntry)
	if !ok {
		return "", false
	}
	return e.command, true
}

// UpdateFilter sets the filter query and rebuilds the filtered list.
func (p *SlashPopup) UpdateFilter(query string) {
	if p.picker == nil {
		return
	}
	p.picker.SetFilter(query)
}

// View renders the popup as plain lines.
func (p *SlashPopup) View(_ int) string {
	if !p.open || p.picker == nil {
		return ""
	}
	return p.picker.View()
}

// Height returns the rendered height of the popup (0 if not open).
func (p *SlashPopup) Height() int {
	if !p.open || p.picker == nil {
		return 0
	}
	return p.picker.Height()
}
