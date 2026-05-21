package dialog

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"cece/internal/ui/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/sahilm/fuzzy"
)

// SessionItem wraps a SessionInfo to implement the list.FilterableItem interface.
type SessionItem struct {
	*list.Versioned
	SessionInfo
	styles           DialogStyles
	sessionsMode     sessionsMode
	m                fuzzy.Match
	cache            map[int]string
	updateTitleInput textinput.Model
	focused          bool
}

var _ list.FilterableItem = (*SessionItem)(nil)

// Finished implements list.Item.
func (s *SessionItem) Finished() bool { return true }

// Filter implements list.FilterableItem.
func (s *SessionItem) Filter() string { return s.Title }

// ID returns the unique identifier of the session.
func (s *SessionItem) ID() string { return s.SessionInfo.ID }

// SetMatch implements list.MatchSettable.
func (s *SessionItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(s.m, m) {
		return
	}
	s.cache = nil
	s.m = m
	if s.Versioned != nil {
		s.Bump()
	}
}

// InputValue returns the updated title value.
func (s *SessionItem) InputValue() string {
	return s.updateTitleInput.Value()
}

// HandleInput forwards input message to the update title input.
func (s *SessionItem) HandleInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	s.updateTitleInput, cmd = s.updateTitleInput.Update(msg)
	return cmd
}

// Cursor returns the cursor of the update title input.
func (s *SessionItem) Cursor() *tea.Cursor {
	return s.updateTitleInput.Cursor()
}

// Render implements list.Item.
func (s *SessionItem) Render(width int) string {
	info := formatTimeAgo(s.UpdatedAt)
	styles := listItemStyles{
		itemBlurred:     s.styles.NormalItem,
		itemFocused:     s.styles.SelectedItem,
		infoTextBlurred: s.styles.InfoBlurred,
		infoTextFocused: s.styles.InfoFocused,
	}

	switch s.sessionsMode {
	case sessionsModeDeleting:
		styles.itemBlurred = s.styles.DeletingItemBlurred
		styles.itemFocused = s.styles.DeletingItemFocused
	case sessionsModeUpdating:
		styles.itemBlurred = s.styles.RenamingItemBlurred
		styles.itemFocused = s.styles.RenamingItemFocused
		if s.focused {
			const cursorPadding = 1
			inputWidth := max(0, width-styles.itemFocused.GetHorizontalFrameSize()-cursorPadding)
			s.updateTitleInput.SetWidth(inputWidth)
			s.updateTitleInput.Placeholder = ansi.Truncate(s.Title, width, "…")
			return styles.itemFocused.Render(s.updateTitleInput.View())
		}
	}

	return renderItem(styles, s.Title, info, s.focused, width, s.cache, &s.m)
}

// SetFocused implements list.Focusable.
func (s *SessionItem) SetFocused(focused bool) {
	if s.focused == focused {
		return
	}
	s.cache = nil
	s.focused = focused
	if s.Versioned != nil {
		s.Bump()
	}
}

func sessionItems(styles DialogStyles, mode sessionsMode, sessions ...SessionInfo) []list.FilterableItem {
	items := make([]list.FilterableItem, len(sessions))
	for i, s := range sessions {
		item := &SessionItem{
			Versioned:    list.NewVersioned(),
			SessionInfo:  s,
			styles:       styles,
			sessionsMode: mode,
		}
		if mode == sessionsModeUpdating {
			item.updateTitleInput = textinput.New()
			item.updateTitleInput.SetVirtualCursor(false)
			item.updateTitleInput.Prompt = ""
			item.updateTitleInput.Focus()
		}
		items[i] = item
	}
	return items
}

type listItemStyles struct {
	itemBlurred     lipgloss.Style
	itemFocused     lipgloss.Style
	infoTextBlurred lipgloss.Style
	infoTextFocused lipgloss.Style
}

func renderItem(t listItemStyles, title string, info string, focused bool, width int, cache map[int]string, m *fuzzy.Match) string {
	if cache == nil {
		cache = make(map[int]string)
	}
	if cached, ok := cache[width]; ok {
		return cached
	}

	style := t.itemBlurred
	if focused {
		style = t.itemFocused
	}

	var infoText string
	var infoWidth int
	if len(info) > 0 {
		infoText = fmt.Sprintf(" %s ", info)
		if focused {
			infoText = t.infoTextFocused.Render(infoText)
		} else {
			infoText = t.infoTextBlurred.Render(infoText)
		}
		infoWidth = lipgloss.Width(infoText)
	}

	title = ansi.Truncate(title, max(0, width-infoWidth), "…")
	titleWidth := lipgloss.Width(title)
	gap := strings.Repeat(" ", max(0, width-titleWidth-infoWidth))
	content := title

	if m != nil && len(m.MatchedIndexes) > 0 {
		var lastPos int
		parts := make([]string, 0)
		ranges := matchedRanges(m.MatchedIndexes)
		for _, rng := range ranges {
			start, stop := bytePosToVisibleCharPos(title, rng)
			if start > lastPos {
				parts = append(parts, ansi.Cut(title, lastPos, start))
			}
			parts = append(
				parts,
				ansi.NewStyle().Underline(true).String(),
				ansi.Cut(title, start, stop+1),
				ansi.NewStyle().Underline(false).String(),
			)
			lastPos = stop + 1
		}
		if lastPos < ansi.StringWidth(title) {
			parts = append(parts, ansi.Cut(title, lastPos, ansi.StringWidth(title)))
		}
		content = strings.Join(parts, "")
	}

	content = style.Render(content + gap + infoText)
	cache[width] = content
	return content
}

func sameFuzzyMatch(a, b fuzzy.Match) bool {
	return a.Str == b.Str &&
		a.Index == b.Index &&
		a.Score == b.Score &&
		slices.Equal(a.MatchedIndexes, b.MatchedIndexes)
}

func matchedRanges(in []int) [][2]int {
	if len(in) == 0 {
		return [][2]int{}
	}
	current := [2]int{in[0], in[0]}
	if len(in) == 1 {
		return [][2]int{current}
	}
	var out [][2]int
	for i := 1; i < len(in); i++ {
		if in[i] == current[1]+1 {
			current[1] = in[i]
		} else {
			out = append(out, current)
			current = [2]int{in[i], in[i]}
		}
	}
	out = append(out, current)
	return out
}

func bytePosToVisibleCharPos(str string, rng [2]int) (int, int) {
	bytePos, byteStart, byteStop := 0, rng[0], rng[1]
	pos, start, stop := 0, 0, 0
	gr := uniseg.NewGraphemes(str)
	for byteStart > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	start = pos
	for byteStop > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	stop = pos
	return start, stop
}

// formatTimeAgo returns a human-readable relative time string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
