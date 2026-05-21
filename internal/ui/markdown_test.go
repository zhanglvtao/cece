package ui

import (
	"strings"
	"testing"
)

func TestRenderMarkdownPlainText(t *testing.T) {
	out := renderMarkdown("Hello world", 80)
	if !strings.Contains(out, "Hello") {
		t.Fatalf("rendered output missing 'Hello': %q", out)
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	input := "```go\nfmt.Println(\"hi\")\n```"
	out := renderMarkdown(input, 80)
	if out == "" {
		t.Fatal("rendered output is empty")
	}
	if out == input {
		t.Fatal("output should differ from raw input (glamour should style it)")
	}
}

func TestRenderMarkdownHeading(t *testing.T) {
	input := "# Title"
	out := renderMarkdown(input, 80)
	if out == "" {
		t.Fatal("rendered output is empty")
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	out := renderMarkdown("", 80)
	if out != "" {
		t.Fatalf("empty input should produce empty output, got %q", out)
	}
}

func TestRenderMarkdownBulletList(t *testing.T) {
	input := "- item one\n- item two\n- item three"
	out := renderMarkdown(input, 80)
	plain := stripAnsi(out)
	if !strings.Contains(plain, "item one") {
		t.Fatalf("rendered output missing 'item one': %q", plain)
	}
}

func TestMarkdownRendererCached(t *testing.T) {
	r1 := markdownRenderer(80)
	r2 := markdownRenderer(80)
	if r1 != r2 {
		t.Fatal("renderer should be cached for same width")
	}
	r3 := markdownRenderer(60)
	if r1 == r3 {
		t.Fatal("renderer should be different for different widths")
	}
}

// stripAnsi removes ANSI escape sequences for test assertions.
func stripAnsi(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x7e {
				j++
				if s[j-1] == 'm' {
					break
				}
			}
			i = j
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
