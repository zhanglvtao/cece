package dialog

import (
	"fmt"
	"slices"
	"strings"

	"cece/internal/chat"
	"cece/internal/ui/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

type ModelPickerItem struct {
	*list.Versioned
	chat.ModelInfo
	styles  DialogStyles
	current bool
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var _ list.FilterableItem = (*ModelPickerItem)(nil)

func (i *ModelPickerItem) Finished() bool { return true }

func (i *ModelPickerItem) Filter() string { return i.ID + " " + i.DisplayName }

func (i *ModelPickerItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(i.m, m) {
		return
	}
	i.cache = nil
	i.m = m
	if i.Versioned != nil {
		i.Bump()
	}
}

func (i *ModelPickerItem) SetFocused(focused bool) {
	if i.focused == focused {
		return
	}
	i.cache = nil
	i.focused = focused
	if i.Versioned != nil {
		i.Bump()
	}
}

func (i *ModelPickerItem) Render(width int) string {
	if i.cache == nil {
		i.cache = make(map[int]string)
	}
	if cached, ok := i.cache[width]; ok {
		return cached
	}

	style := i.styles.NormalItem
	if i.focused {
		style = i.styles.SelectedItem
	}

	ctxLabel := formatContextWindow(i.MaxContextWindow)
	info := i.Provider + " " + ctxLabel
	if i.current {
		info = "● " + info
	}

	var infoText string
	var infoWidth int
	if len(info) > 0 {
		rendered := fmt.Sprintf(" %s ", info)
		if i.focused {
			infoText = i.styles.InfoFocused.Render(rendered)
		} else {
			infoText = i.styles.InfoBlurred.Render(rendered)
		}
		infoWidth = lipgloss.Width(infoText)
	}

	title := i.DisplayName
	if title == "" {
		title = i.ID
	}
	title = ansi.Truncate(title, max(0, width-infoWidth), "…")
	titleWidth := lipgloss.Width(title)
	gap := strings.Repeat(" ", max(0, width-titleWidth-infoWidth))
	content := title

	if i.m.MatchedIndexes != nil && len(i.m.MatchedIndexes) > 0 {
		var lastPos int
		parts := make([]string, 0)
		ranges := matchedRanges(i.m.MatchedIndexes)
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

	result := style.Render(content + gap + infoText)
	i.cache[width] = result
	return result
}

func modelPickerItems(styles DialogStyles, models []chat.ModelInfo, currentModel string) []list.FilterableItem {
	items := make([]list.FilterableItem, len(models))
	for i, m := range models {
		items[i] = &ModelPickerItem{
			Versioned: list.NewVersioned(),
			ModelInfo: m,
			styles:    styles,
			current:   m.ID == currentModel,
		}
	}
	return items
}

func formatContextWindow(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Suppress unused import.
var _ = slices.Equal[[]int, int]
