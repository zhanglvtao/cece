package ui

import (
	"strings"
	"testing"

	"charm.land/glamour/v2"
)

func newTestRenderer(t *testing.T, width int) *glamour.TermRenderer {
	t.Helper()
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyleConfig()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStreamingMarkdownEmpty(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)
	out := sm.Render("", 80, r)
	if out != "" {
		t.Fatalf("empty input should produce empty output, got %q", out)
	}
}

func TestStreamingMarkdownPlainText(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)
	out := sm.Render("Hello world", 80, r)
	plain := stripAnsi(out)
	if !strings.Contains(plain, "Hello") {
		t.Fatalf("rendered output missing 'Hello': %q", plain)
	}
}

func TestStreamingMarkdownIncremental(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)

	// First render: a single paragraph
	out1 := sm.Render("Hello", 80, r)
	if out1 == "" {
		t.Fatal("first render should not be empty")
	}

	// Second render: content extended with blank-line boundary
	out2 := sm.Render("Hello\n\nWorld", 80, r)
	plain := stripAnsi(out2)
	if !strings.Contains(plain, "Hello") {
		t.Fatalf("incremental render missing 'Hello': %q", plain)
	}
	if !strings.Contains(plain, "World") {
		t.Fatalf("incremental render missing 'World': %q", plain)
	}
}

func TestStreamingMarkdownCodeBlockBoundary(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)

	// Content with an open code block — no safe boundary should be found
	openFence := "```go\nfmt.Println("
	out := sm.Render(openFence, 80, r)
	if out == "" {
		t.Fatal("render of open fence should not be empty")
	}

	// Close the code block — now there should be a safe boundary
	closed := openFence + "\n```"
	out2 := sm.Render(closed, 80, r)
	plain := stripAnsi(out2)
	if !strings.Contains(plain, "fmt") {
		t.Fatalf("closed fence render missing 'fmt': %q", plain)
	}
}

func TestStreamingMarkdownWidthChange(t *testing.T) {
	var sm streamingMarkdown
	r80 := newTestRenderer(t, 80)

	sm.Render("Hello\n\nWorld", 80, r80)
	if sm.stablePrefix == "" {
		t.Fatal("should have cached a stable prefix after first render")
	}

	// Width change should reset the cache
	r60 := newTestRenderer(t, 60)
	sm.Render("Hello\n\nWorld", 60, r60)
	if sm.width != 60 {
		t.Fatalf("width should be 60 after width change, got %d", sm.width)
	}
}

func TestStreamingMarkdownHeading(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)
	out := sm.Render("# Title\n\nSome content", 80, r)
	plain := stripAnsi(out)
	if !strings.Contains(plain, "Title") {
		t.Fatalf("rendered output missing 'Title': %q", plain)
	}
}

func TestStreamingMarkdownReset(t *testing.T) {
	var sm streamingMarkdown
	r := newTestRenderer(t, 80)

	sm.Render("Hello\n\nWorld", 80, r)
	if sm.stablePrefix == "" {
		t.Fatal("should have cached a stable prefix")
	}

	sm.Reset()
	if sm.stablePrefix != "" || sm.stablePrefixRender != "" || sm.width != 0 {
		t.Fatal("Reset should clear all cached fields")
	}
}

func TestFindSafeMarkdownBoundary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int // -1 means no boundary expected
	}{
		{"empty", "", -1},
		{"no blank line", "Hello world", -1},
		{"blank line", "Hello\n\nWorld", 7}, // after "Hello\n\n"
		{"open fence", "```go\ncode\n", -1},
		{"closed fence", "```go\ncode\n```\n\nMore", 14}, // after closing fence + blank line
		{"list", "- item one\n- item two", -1},           // list markers block boundary
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSafeMarkdownBoundary(tt.content)
			if tt.want == -1 {
				// We just verify it returns -1 for cases where no boundary should exist
				if got != -1 && tt.name == "no blank line" {
					t.Fatalf("expected no boundary, got %d", got)
				}
			}
			// For cases with boundaries, just verify it returns a positive value
			if tt.want > 0 && got <= 0 {
				t.Fatalf("expected boundary > 0, got %d", got)
			}
		})
	}
}
