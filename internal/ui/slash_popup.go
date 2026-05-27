package ui

import (
	"strings"

	"cece/internal/skill"
	"cece/internal/ui/list"
	"github.com/sahilm/fuzzy"
)

const slashPopupMaxHeight = 8

var builtinSlashCommands = []struct {
	cmd, desc string
}{
	{"/model", "Switch model"},
	{"/resume", "Resume a session"},
	{"/clear", "Clear conversation history"},
	{"/skills", "List available skills"},
}

// slashItem is a single candidate in the slash command popup.
type slashItem struct {
	*list.Versioned
	command     string
	description string
	source      string // "builtin" | "skill"
	focused     bool
	match       fuzzy.Match
}

var (
	_ list.Item           = (*slashItem)(nil)
	_ list.FilterableItem = (*slashItem)(nil)
	_ list.MatchSettable  = (*slashItem)(nil)
	_ list.Focusable      = (*slashItem)(nil)
)

func newSlashItem(cmd, desc, source string) *slashItem {
	return &slashItem{
		Versioned:   list.NewVersioned(),
		command:     cmd,
		description: desc,
		source:      source,
	}
}

func (s *slashItem) Render(width int) string {
	prefix := "  "
	if s.focused {
		prefix = "> "
	}
	text := s.command
	if s.description != "" {
		text += "  " + s.description
	}
	return prefix + text
}

func (s *slashItem) Finished() bool  { return true }
func (s *slashItem) Filter() string   { return strings.TrimPrefix(s.command, "/") }
func (s *slashItem) SetMatch(m fuzzy.Match) { s.match = m; s.Bump() }
func (s *slashItem) SetFocused(focused bool) { s.focused = focused; s.Bump() }

// SlashPopup is the slash command completion popup component.
type SlashPopup struct {
	open  bool
	flist *list.FilterableList
	items []*slashItem
}

// NewSlashPopup creates a new SlashPopup component.
func NewSlashPopup(_ Styles) *SlashPopup { return &SlashPopup{} }

// SetSkills rebuilds the candidate list from builtin commands + skills.
func (p *SlashPopup) SetSkills(skills []*skill.Skill) {
	var items []*slashItem
	for _, b := range builtinSlashCommands {
		items = append(items, newSlashItem(b.cmd, b.desc, "builtin"))
	}
	for _, s := range skills {
		items = append(items, newSlashItem("/"+s.Name, s.Description, "skill"))
	}
	p.items = items
	fitems := make([]list.FilterableItem, len(items))
	for i, item := range items {
		fitems[i] = item
	}
	if p.flist == nil {
		p.flist = list.NewFilterableList(fitems...)
		p.flist.SetSize(0, slashPopupMaxHeight)
		p.flist.Focus()
	} else {
		p.flist.SetItems(fitems...)
	}
}

// Open shows the popup with the given query filter.
func (p *SlashPopup) Open(query string) {
	if p.flist == nil {
		return
	}
	p.open = true
	p.flist.SetFilter(query)
	if p.flist.Len() > 0 {
		p.flist.SetSelected(0)
	}
}

// Close hides the popup.
func (p *SlashPopup) Close() { p.open = false }

// Active returns whether the popup is visible.
func (p *SlashPopup) Active() bool { return p.open }

// UpdateFilter updates the fuzzy filter query.
func (p *SlashPopup) UpdateFilter(query string) {
	if p.flist == nil {
		return
	}
	p.flist.SetFilter(query)
	if p.flist.Len() > 0 && p.flist.Selected() < 0 {
		p.flist.SetSelected(0)
	}
}

// SelectUp moves the selection up.
func (p *SlashPopup) SelectUp() {
	if p.flist == nil {
		return
	}
	p.flist.SelectPrev()
}

// SelectDown moves the selection down.
func (p *SlashPopup) SelectDown() {
	if p.flist == nil {
		return
	}
	p.flist.SelectNext()
}

// SelectedCommand returns the currently selected command string.
func (p *SlashPopup) SelectedCommand() (string, bool) {
	if p.flist == nil {
		return "", false
	}
	item := p.flist.SelectedItem()
	if item == nil {
		return "", false
	}
	si, ok := item.(*slashItem)
	if !ok {
		return "", false
	}
	return si.command, true
}

// View renders the popup as plain lines.
func (p *SlashPopup) View(_ int) string {
	if !p.open || p.flist == nil {
		return ""
	}
	items := p.flist.FilteredItems()
	if len(items) == 0 {
		return ""
	}
	selectedIdx := p.flist.Selected()
	maxItems := min(len(items), slashPopupMaxHeight)
	var b strings.Builder
	for i := 0; i < maxItems; i++ {
		si, ok := items[i].(*slashItem)
		if !ok {
			continue
		}
		prefix := "  "
		text := si.command
		if si.description != "" {
			text += "  " + si.description
		}
		if i == selectedIdx {
			prefix = "> "
		}
		b.WriteString(prefix + text)
		if i < maxItems-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Height returns the rendered height of the popup (0 if not open).
func (p *SlashPopup) Height() int {
	if !p.open {
		return 0
	}
	items := p.flist.FilteredItems()
	return min(len(items), slashPopupMaxHeight)
}
