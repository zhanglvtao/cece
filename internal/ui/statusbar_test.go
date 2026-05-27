package ui

import (
	"testing"
)

func TestStatusBarModelPill(t *testing.T) {
	sb := NewStatusBar(DefaultStyles())
	sb.UpdateModel("sonnet")
	h := sb.Layout(80)
	if h != 1 {
		t.Fatalf("expected height 1, got %d", h)
	}
	// Verify model cell exists
	c, ok := sb.cellMap["model"]
	if !ok {
		t.Fatal("model cell not found")
	}
	if c.width == 0 {
		t.Fatal("model cell has zero width")
	}
}

func TestStatusBarToolCounts(t *testing.T) {
	sb := NewStatusBar(DefaultStyles())
	sb.UpdateModel("sonnet")

	sb.IncrementTool("Grep")
	sb.IncrementTool("Read")
	sb.IncrementTool("Grep") // Grep:2
	sb.ReorderToolCells()

	h := sb.Layout(80)
	if h != 1 {
		t.Fatalf("expected height 1, got %d", h)
	}

	if sb.toolCounts["Grep"] != 2 {
		t.Fatalf("Grep count = %d, want 2", sb.toolCounts["Grep"])
	}
	if sb.toolCounts["Read"] != 1 {
		t.Fatalf("Read count = %d, want 1", sb.toolCounts["Read"])
	}
}

func TestStatusBarFlowLayout(t *testing.T) {
	sb := NewStatusBar(DefaultStyles())
	sb.UpdateModel("sonnet")

	// Add many tools to force wrapping
	for i := 0; i < 20; i++ {
		sb.IncrementTool("Tool" + string(rune('A'+i)))
	}
	sb.ReorderToolCells()

	h := sb.Layout(40) // narrow width to force wrap
	if h < 2 {
		t.Fatalf("expected multi-line layout, got height %d", h)
	}
}

func TestStatusBarScroll(t *testing.T) {
	sb := NewStatusBar(DefaultStyles())
	sb.UpdateModel("sonnet")
	sb.UpdateScroll(42)

	h := sb.Layout(80)
	if h != 1 {
		t.Fatalf("expected height 1, got %d", h)
	}

	c, ok := sb.cellMap["scroll"]
	if !ok {
		t.Fatal("scroll cell not found")
	}
	if c.width == 0 {
		t.Fatal("scroll cell has zero width")
	}

	// Zero scroll should hide the cell
	sb.UpdateScroll(0)
	sb.Layout(80)
	if c.width != 0 {
		t.Fatal("scroll cell should have zero width when scroll is 0")
	}
}

func TestStatusBarResetToolCounts(t *testing.T) {
	sb := NewStatusBar(DefaultStyles())
	sb.UpdateModel("sonnet")
	sb.IncrementTool("Grep")
	sb.IncrementTool("Read")
	sb.ReorderToolCells()

	sb.ResetToolCounts()

	if len(sb.toolCounts) != 0 {
		t.Fatal("tool counts should be empty after reset")
	}
	if _, ok := sb.cellMap["tool_Grep"]; ok {
		t.Fatal("tool_Grep cell should be removed after reset")
	}
}
