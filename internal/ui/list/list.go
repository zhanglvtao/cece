package list

import (
	"strings"
)

// List represents a list of items that can be lazily rendered. A list is
// always rendered like a chat conversation where items are stacked vertically
// from top to bottom.
type List struct {
	width, height int
	items         []Item
	gap           int
	reverse       bool

	focused     bool
	selectedIdx int

	offsetIdx  int
	offsetLine int

	renderCallbacks []RenderCallback

	cache            map[Item]*listCacheEntry
	freezeSuppressed map[Item]struct{}
}

type listCacheEntry struct {
	width   int
	version uint64
	frozen  bool
	content string
	lines   []string
	height  int
}

type renderedItem struct {
	content string
	height  int
}

// NewList creates a new lazy-loaded list.
func NewList(items ...Item) *List {
	l := new(List)
	l.items = items
	l.selectedIdx = -1
	l.cache = make(map[Item]*listCacheEntry)
	l.freezeSuppressed = make(map[Item]struct{})
	return l
}

// RenderCallback defines a function that can modify an item before it is
// rendered.
type RenderCallback func(idx, selectedIdx int, item Item) Item

// RegisterRenderCallback registers a callback to be called when rendering
// items.
func (l *List) RegisterRenderCallback(cb RenderCallback) {
	l.renderCallbacks = append(l.renderCallbacks, cb)
}

// SetSize sets the size of the list viewport.
func (l *List) SetSize(width, height int) {
	if l.width != width {
		l.invalidateAll()
	}
	l.width = width
	l.height = height
}

// SetGap sets the gap between items.
func (l *List) SetGap(gap int) {
	l.gap = gap
}

// Gap returns the gap between items.
func (l *List) Gap() int {
	return l.gap
}

// AtBottom returns whether the list is showing the last item at the bottom.
func (l *List) AtBottom() bool {
	if len(l.items) == 0 {
		return true
	}
	var totalHeight int
	for idx := l.offsetIdx; idx < len(l.items); idx++ {
		if totalHeight > l.height {
			return false
		}
		item := l.getItem(idx)
		itemHeight := item.height
		if l.gap > 0 && idx > l.offsetIdx {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
	}
	return totalHeight-l.offsetLine <= l.height
}

// SetReverse shows the list in reverse order.
func (l *List) SetReverse(reverse bool) {
	l.reverse = reverse
}

// Width returns the width of the list viewport.
func (l *List) Width() int {
	return l.width
}

// Height returns the height of the list viewport.
func (l *List) Height() int {
	return l.height
}

// Len returns the number of items in the list.
func (l *List) Len() int {
	return len(l.items)
}

func (l *List) lastOffsetItem() (int, int, int) {
	var totalHeight int
	var idx int
	for idx = len(l.items) - 1; idx >= 0; idx-- {
		item := l.getItem(idx)
		itemHeight := item.height
		if l.gap > 0 && idx < len(l.items)-1 {
			itemHeight += l.gap
		}
		totalHeight += itemHeight
		if totalHeight > l.height {
			break
		}
	}
	lineOffset := max(totalHeight-l.height, 0)
	idx = max(idx, 0)
	return idx, lineOffset, totalHeight
}

func (l *List) getItem(idx int) renderedItem {
	if idx < 0 || idx >= len(l.items) {
		return renderedItem{}
	}
	entry := l.renderItemEntry(idx)
	if entry == nil {
		return renderedItem{}
	}
	return renderedItem{content: entry.content, height: entry.height}
}

func (l *List) renderItemEntry(idx int) *listCacheEntry {
	if idx < 0 || idx >= len(l.items) {
		return nil
	}

	rawItem := l.items[idx]
	entry := l.cache[rawItem]

	item := rawItem
	if len(l.renderCallbacks) > 0 {
		for _, cb := range l.renderCallbacks {
			if it := cb(idx, l.selectedIdx, item); it != nil {
				item = it
			}
		}
	}

	version := rawItem.Version()
	if entry != nil && entry.width == l.width && entry.version == version {
		if !entry.frozen {
			return entry
		}
		if _, suppressed := l.freezeSuppressed[rawItem]; !suppressed {
			return entry
		}
	}

	rendered := item.Render(l.width)
	rendered = strings.TrimRight(rendered, "\n")
	lines := strings.Split(rendered, "\n")
	height := len(lines)

	finalVersion := rawItem.Version()

	frozen := false
	if rawItem.Finished() {
		if _, suppressed := l.freezeSuppressed[rawItem]; !suppressed {
			frozen = true
		}
	}

	if entry == nil {
		entry = &listCacheEntry{}
		l.cache[rawItem] = entry
	}
	entry.width = l.width
	entry.version = finalVersion
	entry.frozen = frozen
	entry.content = rendered
	entry.lines = lines
	entry.height = height
	return entry
}

func (l *List) invalidateAll() {
	for k := range l.cache {
		delete(l.cache, k)
	}
}

// Invalidate drops the cache entry for the given item.
func (l *List) Invalidate(item Item) {
	delete(l.cache, item)
}

// InvalidateFrozen drops the frozen flag for the given item.
func (l *List) InvalidateFrozen(item Item) {
	delete(l.cache, item)
}

func (l *List) retainCacheFor(items []Item) {
	if len(l.cache) == 0 {
		return
	}
	keep := make(map[Item]struct{}, len(items))
	for _, it := range items {
		keep[it] = struct{}{}
	}
	for k := range l.cache {
		if _, ok := keep[k]; !ok {
			delete(l.cache, k)
		}
	}
}

// BeginSelectionDrag marks items in the inclusive [startIdx, endIdx]
// range as un-freezable for the duration of an active selection drag.
func (l *List) BeginSelectionDrag(startIdx, endIdx int) {
	if len(l.items) == 0 {
		return
	}
	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}
	startIdx = max(startIdx, 0)
	endIdx = min(endIdx, len(l.items)-1)
	for i := startIdx; i <= endIdx; i++ {
		it := l.items[i]
		l.freezeSuppressed[it] = struct{}{}
		if entry, ok := l.cache[it]; ok && entry.frozen {
			delete(l.cache, it)
		}
	}
}

// EndSelectionDrag clears the selection-drag freeze suppression.
func (l *List) EndSelectionDrag() {
	for k := range l.freezeSuppressed {
		delete(l.freezeSuppressed, k)
		delete(l.cache, k)
	}
}

// ScrollToIndex scrolls the list to the given item index.
func (l *List) ScrollToIndex(index int) {
	if index < 0 {
		index = 0
	}
	if index >= len(l.items) {
		index = len(l.items) - 1
	}
	l.offsetIdx = index
	l.offsetLine = 0
}

// ScrollBy scrolls the list by the given number of lines.
func (l *List) ScrollBy(lines int) {
	if len(l.items) == 0 || lines == 0 {
		return
	}
	if l.reverse {
		lines = -lines
	}
	if lines > 0 {
		if l.AtBottom() {
			return
		}
		l.offsetLine += lines
		currentItem := l.getItem(l.offsetIdx)
		for l.offsetLine >= currentItem.height {
			l.offsetLine -= currentItem.height
			if l.gap > 0 {
				l.offsetLine = max(0, l.offsetLine-l.gap)
			}
			l.offsetIdx++
			if l.offsetIdx > len(l.items)-1 {
				l.ScrollToBottom()
				return
			}
			currentItem = l.getItem(l.offsetIdx)
		}
		lastOffsetIdx, lastOffsetLine, _ := l.lastOffsetItem()
		if l.offsetIdx > lastOffsetIdx || (l.offsetIdx == lastOffsetIdx && l.offsetLine > lastOffsetLine) {
			l.offsetIdx = lastOffsetIdx
			l.offsetLine = lastOffsetLine
		}
	} else if lines < 0 {
		l.offsetLine += lines
		for l.offsetLine < 0 {
			l.offsetIdx--
			if l.offsetIdx < 0 {
				l.ScrollToTop()
				break
			}
			prevItem := l.getItem(l.offsetIdx)
			totalHeight := prevItem.height
			if l.gap > 0 {
				totalHeight += l.gap
			}
			l.offsetLine += totalHeight
		}
	}
}

// VisibleItemIndices finds the range of items that are visible in the viewport.
func (l *List) VisibleItemIndices() (startIdx, endIdx int) {
	if len(l.items) == 0 {
		return 0, 0
	}
	startIdx = l.offsetIdx
	currentIdx := startIdx
	visibleHeight := -l.offsetLine

	for currentIdx < len(l.items) {
		item := l.getItem(currentIdx)
		visibleHeight += item.height
		if l.gap > 0 {
			visibleHeight += l.gap
		}
		if visibleHeight >= l.height {
			break
		}
		currentIdx++
	}

	endIdx = currentIdx
	if endIdx >= len(l.items) {
		endIdx = len(l.items) - 1
	}
	return startIdx, endIdx
}

// Render renders the list and returns the visible lines.
func (l *List) Render() string {
	if len(l.items) == 0 {
		return ""
	}

	budget := max(l.height, 0)
	lines := make([]string, 0, budget)
	currentIdx := l.offsetIdx
	currentOffset := l.offsetLine

	for currentIdx < len(l.items) {
		remaining := budget - len(lines)
		if remaining <= 0 {
			break
		}

		entry := l.renderItemEntry(currentIdx)
		if entry == nil {
			break
		}
		itemLines := entry.lines
		itemHeight := len(itemLines)

		if currentOffset >= 0 && currentOffset < itemHeight {
			visible := itemLines[currentOffset:]
			if len(visible) > remaining {
				visible = visible[:remaining]
			}
			lines = append(lines, visible...)

			if l.gap > 0 {
				gapBudget := min(budget-len(lines), l.gap)
				for range gapBudget {
					lines = append(lines, "")
				}
			}
		} else {
			gapOffset := currentOffset - itemHeight
			gapRemaining := l.gap - gapOffset
			if gapRemaining > 0 {
				gapBudget := min(budget-len(lines), gapRemaining)
				for range gapBudget {
					lines = append(lines, "")
				}
			}
		}

		currentIdx++
		currentOffset = 0
	}

	l.height = budget

	if l.reverse {
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// PrependItems prepends items to the list.
func (l *List) PrependItems(items ...Item) {
	l.items = append(items, l.items...)
	l.offsetIdx += len(items)
	if l.selectedIdx != -1 {
		l.selectedIdx += len(items)
	}
}

// SetItems sets the items in the list. Cache entries for items that
// remain after the swap are preserved.
func (l *List) SetItems(items ...Item) {
	l.items = items
	l.selectedIdx = min(l.selectedIdx, len(l.items)-1)
	l.offsetIdx = min(l.offsetIdx, len(l.items)-1)
	l.offsetLine = 0
	l.retainCacheFor(items)
}

// AppendItems appends items to the list.
func (l *List) AppendItems(items ...Item) {
	l.items = append(l.items, items...)
}

// RemoveItem removes the item at the given index from the list.
func (l *List) RemoveItem(idx int) {
	if idx < 0 || idx >= len(l.items) {
		return
	}
	removed := l.items[idx]
	l.items = append(l.items[:idx], l.items[idx+1:]...)
	delete(l.cache, removed)
	delete(l.freezeSuppressed, removed)

	if l.selectedIdx == idx {
		l.selectedIdx = -1
	} else if l.selectedIdx > idx {
		l.selectedIdx--
	}

	if l.offsetIdx > idx {
		l.offsetIdx--
	} else if l.offsetIdx == idx && l.offsetIdx >= len(l.items) {
		l.offsetIdx = max(0, len(l.items)-1)
		l.offsetLine = 0
	}
}

// Focused returns whether the list is focused.
func (l *List) Focused() bool {
	return l.focused
}

// Focus sets the focus state of the list.
func (l *List) Focus() {
	l.focused = true
}

// Blur removes the focus state from the list.
func (l *List) Blur() {
	l.focused = false
}

// ScrollToTop scrolls the list to the top.
func (l *List) ScrollToTop() {
	l.offsetIdx = 0
	l.offsetLine = 0
}

// ScrollToBottom scrolls the list to the bottom.
func (l *List) ScrollToBottom() {
	if len(l.items) == 0 {
		return
	}
	lastOffsetIdx, lastOffsetLine, _ := l.lastOffsetItem()
	l.offsetIdx = lastOffsetIdx
	l.offsetLine = lastOffsetLine
}

// ScrollToSelected scrolls the list to the selected item.
func (l *List) ScrollToSelected() {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return
	}
	startIdx, endIdx := l.VisibleItemIndices()
	if l.selectedIdx < startIdx {
		l.offsetIdx = l.selectedIdx
		l.offsetLine = 0
	} else if l.selectedIdx > endIdx {
		var totalHeight int
		for i := l.selectedIdx; i >= 0; i-- {
			item := l.getItem(i)
			totalHeight += item.height
			if l.gap > 0 && i < l.selectedIdx {
				totalHeight += l.gap
			}
			if totalHeight >= l.height {
				l.offsetIdx = i
				l.offsetLine = totalHeight - l.height
				break
			}
		}
		if totalHeight < l.height {
			l.ScrollToTop()
		}
	}
}

// SelectedItemInView returns whether the selected item is currently in view.
func (l *List) SelectedItemInView() bool {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return false
	}
	startIdx, endIdx := l.VisibleItemIndices()
	return l.selectedIdx >= startIdx && l.selectedIdx <= endIdx
}

// SetSelected sets the selected item index in the list.
func (l *List) SetSelected(index int) {
	if index < 0 || index >= len(l.items) {
		l.selectedIdx = -1
	} else {
		l.selectedIdx = index
	}
}

// Selected returns the index of the currently selected item.
func (l *List) Selected() int {
	return l.selectedIdx
}

// IsSelectedFirst returns whether the first item is selected.
func (l *List) IsSelectedFirst() bool {
	return l.selectedIdx == 0
}

// IsSelectedLast returns whether the last item is selected.
func (l *List) IsSelectedLast() bool {
	return l.selectedIdx == len(l.items)-1
}

// SelectPrev selects the visually previous item.
func (l *List) SelectPrev() bool {
	if l.reverse {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	} else {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	}
	return false
}

// SelectNext selects the next item in the list.
func (l *List) SelectNext() bool {
	if l.reverse {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	} else {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	}
	return false
}

// SelectFirst selects the first item in the list.
func (l *List) SelectFirst() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = 0
	return true
}

// SelectLast selects the last item in the list.
func (l *List) SelectLast() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = len(l.items) - 1
	return true
}

// WrapToStart wraps selection to the visual start.
func (l *List) WrapToStart() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = len(l.items) - 1
	} else {
		l.selectedIdx = 0
	}
	return true
}

// WrapToEnd wraps selection to the visual end.
func (l *List) WrapToEnd() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = 0
	} else {
		l.selectedIdx = len(l.items) - 1
	}
	return true
}

// SelectedItem returns the currently selected item.
func (l *List) SelectedItem() Item {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SelectFirstInView selects the first item currently in view.
func (l *List) SelectFirstInView() {
	startIdx, _ := l.VisibleItemIndices()
	l.selectedIdx = startIdx
}

// SelectLastInView selects the last item currently in view.
func (l *List) SelectLastInView() {
	_, endIdx := l.VisibleItemIndices()
	l.selectedIdx = endIdx
}

// ItemAt returns the item at the given index.
func (l *List) ItemAt(index int) Item {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// ItemIndexAtPosition returns the item at the given viewport-relative y
// coordinate.
func (l *List) ItemIndexAtPosition(x, y int) (itemIdx int, itemY int) {
	return l.findItemAtY(x, y)
}

func (l *List) findItemAtY(_, y int) (itemIdx int, itemY int) {
	if y < 0 || y >= l.height {
		return -1, -1
	}
	currentIdx := l.offsetIdx
	currentLine := -l.offsetLine

	for currentIdx < len(l.items) && currentLine < l.height {
		item := l.getItem(currentIdx)
		itemEndLine := currentLine + item.height

		if y >= currentLine && y < itemEndLine {
			itemY = y - currentLine
			return currentIdx, itemY
		}

		currentLine = itemEndLine
		if l.gap > 0 {
			currentLine += l.gap
		}
		currentIdx++
	}

	return -1, -1
}
