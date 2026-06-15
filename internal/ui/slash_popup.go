package ui

import (
	"sort"

	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/ui/picker"
)

const slashPopupMaxHeight = 8

var builtinSlashCommands = []struct {
	cmd, desc string
}{
	{"/model", "Switch model (provider:model_id)"},
	{"/resume", "Resume a session"},
	{"/clear", "Clear conversation history"},
	{"/compact", "Compress conversation history"},
	{"/truncate-tool-result", "Truncate all tool results"},
	{"/title", "Generate session title"},
	{"/plan", "View latest plan"},
	{"/dryrun", "Preview full request"},
	{"/skills", "List available skills"},
	{"/view", "View file content"},
	{"/mcp", "Manage MCP servers"},
	{"/tool", "List registered tools"},
	{"/exit", "Exit without generating title"},
}

// slashEntry is a single slash command candidate.
type slashEntry struct {
	command     string
	description string
	source      string // "builtin" | "skill"
}

// SlashPopup wraps a compact Picker for slash command completion.
type SlashPopup struct {
	picker  *picker.Picker
	entries []slashEntry
	styles  Styles
	open    bool
}

// NewSlashPopup creates a new SlashPopup component.
func NewSlashPopup(sty Styles) *SlashPopup {
	return &SlashPopup{styles: sty}
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

// matchPriority returns a sort priority for a slashEntry against a query.
// Lower value = higher priority. -1 means no match.
//
//	0 = command prefix match (e.g. query "vi" matches "/view")
//	1 = command contains match (non-prefix)
//	2 = description prefix match
//	3 = description contains match (non-prefix)
func matchPriority(e slashEntry, q string) int {
	if q == "" {
		return 0
	}
	if hasPrefixFold(e.command, q) {
		return 0
	}
	if containsFold(e.command, q) {
		return 1
	}
	if hasPrefixFold(e.description, q) {
		return 2
	}
	if containsFold(e.description, q) {
		return 3
	}
	return -1
}

// filterAndSort filters entries by query and sorts by match priority.
// Entries with the same priority keep their original order (stable).
func filterAndSort(entries []slashEntry, q string) []slashEntry {
	type indexed struct {
		entry    slashEntry
		priority int
		origIdx  int
	}
	var matched []indexed
	for i, e := range entries {
		p := matchPriority(e, q)
		if p >= 0 {
			matched = append(matched, indexed{entry: e, priority: p, origIdx: i})
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].priority < matched[j].priority
	})
	result := make([]slashEntry, len(matched))
	for i, m := range matched {
		result[i] = m.entry
	}
	return result
}

// buildPicker creates the internal picker with the given (pre-filtered & sorted) entries.
func (p *SlashPopup) buildPicker(entries []slashEntry) {
	items := make([]any, len(entries))
	for i, e := range entries {
		items[i] = e
	}
	pk := picker.New("", items, slashPopupMaxHeight, func(item any, selected bool) string {
		e := item.(slashEntry)
		text := e.command
		if e.description != "" {
			text += "  " + e.description
		}
		return styledPickerItem(p.styles.Picker.Cursor, p.styles.Picker.Item, p.styles.Picker.SelectedItem, text, selected)
	})
	pk.SetCompact(true)
	p.picker = pk
}

// hasPrefixFold reports whether s starts with prefix, case-insensitive.
func hasPrefixFold(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return stringsEqualFold(s[:len(prefix)], prefix)
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
	filtered := filterAndSort(p.entries, query)
	p.buildPicker(filtered)
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
	filtered := filterAndSort(p.entries, query)
	p.buildPicker(filtered)
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
