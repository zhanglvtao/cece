package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// ── StatusBarCell ──────────────────────────────────────────────────────────

// StatusBarCell is an independent rendering unit in the status bar.
// Each cell renders itself and tracks its own dirty state so that
// only changed cells are re-rendered.
type StatusBarCell struct {
	id     string       // unique identifier, e.g. "model", "tool_grep", "scroll"
	render func() string // returns styled content
	cached string       // last rendered output
	width  int          // lipgloss.Width of cached
	dirty  bool
}

func newCell(id string, render func() string) *StatusBarCell {
	c := &StatusBarCell{id: id, render: render, dirty: true}
	c.refresh()
	return c
}

func (c *StatusBarCell) refresh() {
	c.cached = c.render()
	c.width = lipgloss.Width(c.cached)
	c.dirty = false
}

func (c *StatusBarCell) markDirty() { c.dirty = true }

// ── StatusBar ──────────────────────────────────────────────────────────────

// StatusBar is a flow-layout status bar composed of independent cells.
// Each cell updates independently; only dirty cells are re-rendered.
type StatusBar struct {
	styles Styles
	cells  []*StatusBarCell

	// cell lookup by id
	cellMap map[string]*StatusBarCell

	// layout cache
	width  int
	height int

	// data backing cells
	modelName  string
	toolCounts map[string]int
	scrollPct  int // 0-100, 0 means at-bottom / no indicator
}

// NewStatusBar creates a StatusBar with default styles.
func NewStatusBar(sty Styles) *StatusBar {
	return &StatusBar{
		styles:     sty,
		cellMap:    make(map[string]*StatusBarCell),
		toolCounts: make(map[string]int),
	}
}

// UpdateModel updates the model pill cell.
func (sb *StatusBar) UpdateModel(name string) {
	sb.modelName = name
	if c, ok := sb.cellMap["model"]; ok {
		c.markDirty()
	} else {
		c := newCell("model", sb.renderModel)
		sb.cells = append(sb.cells, c)
		sb.cellMap["model"] = c
	}
}

// IncrementTool increments the tool count for the given tool name and marks the cell dirty.
func (sb *StatusBar) IncrementTool(name string) {
	sb.toolCounts[name]++
	if c, ok := sb.cellMap["tool_"+name]; ok {
		c.markDirty()
	} else {
		c := newCell("tool_"+name, sb.toolRenderFunc(name))
		sb.cells = append(sb.cells, c)
		sb.cellMap["tool_"+name] = c
	}
}

// UpdateScroll updates the scroll indicator cell.
func (sb *StatusBar) UpdateScroll(percent int) {
	sb.scrollPct = percent
	if c, ok := sb.cellMap["scroll"]; ok {
		c.markDirty()
	} else {
		c := newCell("scroll", sb.renderScroll)
		sb.cells = append(sb.cells, c)
		sb.cellMap["scroll"] = c
	}
}

// ResetToolCounts clears all tool counts (e.g. on session reset).
func (sb *StatusBar) ResetToolCounts() {
	filtered := sb.cells[:0]
	for _, c := range sb.cells {
		if strings.HasPrefix(c.id, "tool_") {
			delete(sb.cellMap, c.id)
		} else {
			filtered = append(filtered, c)
		}
	}
	sb.cells = filtered
	for k := range sb.toolCounts {
		delete(sb.toolCounts, k)
	}
}

// Layout computes the flow layout for all cells within the given width.
// It returns the total height in lines. Call this before Render.
func (sb *StatusBar) Layout(width int) int {
	if width <= 0 {
		width = 80
	}
	sb.width = width

	// Refresh all dirty cells
	for _, c := range sb.cells {
		if c.dirty {
			c.refresh()
		}
	}

	// Count visible cells (non-zero width)
	visible := 0
	for _, c := range sb.cells {
		if c.width > 0 {
			visible++
		}
	}

	if visible == 0 {
		sb.height = 0
		return 0
	}

	// Flow layout: cells left-to-right with 1-space gap, wrap when needed.
	x, lines := 0, 1
	for _, c := range sb.cells {
		if c.width <= 0 {
			continue
		}
		need := c.width
		if x > 0 {
			need++ // gap
		}
		if x+need > width {
			x = 0
			lines++
			need = c.width
		}
		x += need
	}

	sb.height = lines
	return sb.height
}

// Height returns the last computed height (after Layout).
func (sb *StatusBar) Height() int { return sb.height }

// Render returns the flow-layout string for all cells.
// Call Layout first to compute dimensions.
func (sb *StatusBar) Render(width int) string {
	if width <= 0 {
		width = 80
	}
	sb.Layout(width)

	var lines []string
	var line strings.Builder
	x := 0

	for _, c := range sb.cells {
		if c.width <= 0 {
			continue
		}
		need := c.width
		if x > 0 {
			need++ // gap
		}
		if x+need > width && x > 0 {
			lines = append(lines, line.String())
			line.Reset()
			x = 0
			need = c.width
		}
		if x > 0 {
			line.WriteString(" ")
		}
		line.WriteString(c.cached)
		x += need
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}

	return strings.Join(lines, "\n")
}

// ── Cell renderers ─────────────────────────────────────────────────────────

func (sb *StatusBar) renderModel() string {
	if sb.modelName == "" {
		return ""
	}
	return sb.styles.StatusBar.ModelPill.Render(" " + sb.modelName + " ")
}

func (sb *StatusBar) renderScroll() string {
	if sb.scrollPct <= 0 {
		return ""
	}
	return sb.styles.StatusBar.Scroll.Render(fmt.Sprintf("scroll:%d%%", sb.scrollPct))
}

func (sb *StatusBar) toolRenderFunc(name string) func() string {
	return func() string {
		count := sb.toolCounts[name]
		if count <= 0 {
			return ""
		}
		return sb.styles.StatusBar.ToolCount.Render(fmt.Sprintf("%s:%d", name, count))
	}
}

// ReorderToolCells ensures tool cells are in sorted order.
// Call after IncrementTool if you need stable ordering.
func (sb *StatusBar) ReorderToolCells() {
	var nonTool, tool []*StatusBarCell
	for _, c := range sb.cells {
		if strings.HasPrefix(c.id, "tool_") {
			tool = append(tool, c)
		} else {
			nonTool = append(nonTool, c)
		}
	}
	sort.Slice(tool, func(i, j int) bool {
		return tool[i].id < tool[j].id
	})
	sb.cells = sb.cells[:0]
	for _, c := range nonTool {
		if c.id == "model" {
			sb.cells = append(sb.cells, c)
		}
	}
	sb.cells = append(sb.cells, tool...)
	for _, c := range nonTool {
		if c.id == "scroll" {
			sb.cells = append(sb.cells, c)
		}
	}
}
