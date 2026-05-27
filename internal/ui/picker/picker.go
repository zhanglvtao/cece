package picker

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Result is returned by HandleKey to indicate what happened.
type Result int

const (
	ResultNone    Result = iota // key consumed, no further action
	ResultClose                 // esc pressed, caller should close the picker
)

// Picker is a minimal, scrollable list picker for modal dialogs.
type Picker struct {
	title     string
	items     []any
	render    func(any, bool) string // (item, selected) -> one-line text
	filterFn  func(any, string) bool // optional filter predicate
	onSelect  func(any) tea.Cmd      // enter callback
	helpText  string
	selectedI int
	offset    int
	filter    string
	maxHeight int
}

// New creates a Picker with the given title, items, max rendered height,
// and render function. maxHeight includes the title line and help line.
func New(title string, items []any, maxHeight int, render func(any, bool) string) *Picker {
	return &Picker{
		title:     title,
		items:     items,
		render:    render,
		maxHeight: maxHeight,
		helpText:  "[up/down] move  [enter] select  [esc] close",
	}
}

// SetFilterFn sets an optional filter predicate. When set, the picker
// supports text filtering via keyboard input.
func (p *Picker) SetFilterFn(fn func(any, string) bool) { p.filterFn = fn }

// SetOnSelect sets the callback invoked when the user presses enter.
func (p *Picker) SetOnSelect(fn func(any) tea.Cmd) { p.onSelect = fn }

// SetHelpText overrides the bottom help line.
func (p *Picker) SetHelpText(s string) { p.helpText = s }

// visibleItems returns items after applying the current filter.
func (p *Picker) visibleItems() []any {
	if p.filterFn == nil || p.filter == "" {
		return p.items
	}
	var out []any
	for _, item := range p.items {
		if p.filterFn(item, p.filter) {
			out = append(out, item)
		}
	}
	return out
}

// visibleCount is the number of item lines that fit in the viewport.
func (p *Picker) visibleCount() int {
	return max(p.maxHeight-2, 1) // title + help = 2 fixed lines
}

// ensureVisible adjusts offset so selectedI is within the viewport.
func (p *Picker) ensureVisible(total int) {
	if total == 0 {
		return
	}
	vc := p.visibleCount()
	if p.selectedI < p.offset {
		p.offset = p.selectedI
	}
	if p.selectedI >= p.offset+vc {
		p.offset = p.selectedI - vc + 1
	}
	// clamp offset
	if p.offset > total-vc && total > vc {
		p.offset = total - vc
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

// View renders the picker as plain text lines. Total lines ≤ maxHeight.
func (p *Picker) View() string {
	items := p.visibleItems()
	var b strings.Builder

	// Title line
	b.WriteString(p.title)
	if p.filterFn != nil && p.filter != "" {
		b.WriteString(" filter: " + p.filter)
	}
	b.WriteByte('\n')

	// Item lines (virtual scroll window)
	if len(items) == 0 {
		b.WriteString("No items\n")
	} else {
		p.ensureVisible(len(items))
		vc := p.visibleCount()
		end := min(p.offset+vc, len(items))
		for i := p.offset; i < end; i++ {
			b.WriteString(p.render(items[i], i == p.selectedI))
			b.WriteByte('\n')
		}
	}

	// Help line
	b.WriteString(p.helpText)
	return b.String()
}

// Height returns the rendered height in lines (0 if no items).
func (p *Picker) Height() int {
	if len(p.items) == 0 {
		return 0
	}
	items := p.visibleItems()
	return min(len(items)+2, p.maxHeight) // +2 for title and help
}

// Up moves selection up.
func (p *Picker) Up() {
	items := p.visibleItems()
	if len(items) == 0 {
		return
	}
	p.selectedI = (p.selectedI - 1 + len(items)) % len(items)
}

// Down moves selection down.
func (p *Picker) Down() {
	items := p.visibleItems()
	if len(items) == 0 {
		return
	}
	p.selectedI = (p.selectedI + 1) % len(items)
}

// Selected returns the currently selected item.
func (p *Picker) Selected() any {
	items := p.visibleItems()
	if len(items) == 0 {
		return nil
	}
	if p.selectedI < 0 || p.selectedI >= len(items) {
		return nil
	}
	return items[p.selectedI]
}

// HandleKey processes a key event and returns a Result and an optional tea.Cmd.
// The caller should check ResultClose to close the picker, and forward the
// tea.Cmd if non-nil.
func (p *Picker) HandleKey(msg tea.KeyPressMsg) (Result, tea.Cmd) {
	items := p.visibleItems()
	switch msg.String() {
	case "esc":
		return ResultClose, nil
	case "up", "ctrl+p":
		p.Up()
	case "down", "ctrl+n":
		p.Down()
	case "enter", "tab":
		if len(items) == 0 {
			return ResultNone, nil
		}
		selected := p.Selected()
		if p.onSelect != nil {
			return ResultNone, p.onSelect(selected)
		}
		return ResultNone, nil
	default:
		// Filter input (only if filterFn is set)
		if p.filterFn != nil {
			switch msg.String() {
			case "backspace":
				if p.filter != "" {
					_, size := utf8.DecodeLastRuneInString(p.filter)
					p.filter = p.filter[:len(p.filter)-size]
					p.selectedI = 0
					p.offset = 0
				}
			default:
				if text := msg.Key().Text; text != "" {
					p.filter += text
					p.selectedI = 0
					p.offset = 0
				}
			}
			return ResultNone, nil
		}
		return ResultNone, nil
	}
	return ResultNone, nil
}

// FormatItem is a helper that returns a formatted item line with cursor prefix.
func FormatItem(text string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	return fmt.Sprintf("%s%s", cursor, text)
}
