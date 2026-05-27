package picker

import (
	"strings"
	"testing"
)

func TestViewLimitsHeight(t *testing.T) {
	// 20 items, maxHeight=6 → separator(1) + title(1) + visible(2) + help(1) = 5 lines, capped at 6
	items := make([]any, 20)
	for i := range items {
		items[i] = i
	}
	p := New("Test", items, 6, func(item any, selected bool) string {
		return FormatItem(string(rune('a'+item.(int))), selected)
	})

	view := p.View()
	lines := strings.Split(view, "\n")
	// separator + title + items + help, but capped by maxHeight
	if p.Height() != 6 {
		t.Fatalf("Height() = %d, want 6", p.Height())
	}
	if len(lines) > 6 {
		t.Fatalf("got %d lines, want ≤6; view:\n%s", len(lines), view)
	}
}

func TestVirtualScroll(t *testing.T) {
	items := make([]any, 20)
	for i := range items {
		items[i] = i
	}
	p := New("Test", items, 8, func(item any, selected bool) string {
		return FormatItem(string(rune('a'+item.(int))), selected)
	})

	// Initially: items 0-4 visible (maxHeight=8, fixed=3, visibleCount=5), 0 selected
	view := p.View()
	if !strings.Contains(view, "> a") {
		t.Fatalf("initial view should show item 0 selected:\n%s", view)
	}

	// Move down 10 times: selected=10, should scroll
	for i := 0; i < 10; i++ {
		p.Down()
	}
	view = p.View()
	// Item 10 should be visible and selected
	if !strings.Contains(view, "> k") {
		t.Fatalf("after 10 downs, item 10 (k) should be selected:\n%s", view)
	}
}

func TestEmptyItems(t *testing.T) {
	p := New("Test", nil, 6, func(item any, selected bool) string { return "" })
	if p.Height() != 0 {
		t.Fatalf("Height() = %d, want 0 for empty items", p.Height())
	}
	view := p.View()
	if view != "" {
		t.Fatalf("empty picker should return empty view, got:\n%s", view)
	}
}

func TestFilter(t *testing.T) {
	items := []any{"apple", "banana", "apricot", "cherry"}
	p := New("Test", items, 14, func(item any, selected bool) string {
		return FormatItem(item.(string), selected)
	})
	p.SetFilterFn(func(item any, q string) bool {
		return strings.Contains(item.(string), q)
	})

	p.filter = "ap"
	view := p.View()
	if !strings.Contains(view, "apple") || !strings.Contains(view, "apricot") {
		t.Fatalf("filtered view should show apple and apricot:\n%s", view)
	}
	if strings.Contains(view, "banana") {
		t.Fatalf("filtered view should not show banana:\n%s", view)
	}
}
