package ui

import (
	"strings"
	"testing"

	"github.com/zhanglvtao/cece/internal/ui/theme"
)

func TestRenderMarkdownUsesNeutralMainStyle(t *testing.T) {
	invalidateMarkdownCache()
	rendered := renderMarkdown("# Title\n\nA [link](https://example.com) and `code`.", 80)
	for _, themed := range []string{theme.MdHeading, theme.MdLink, theme.MdCode, theme.MdCodeBg} {
		if strings.Contains(rendered, themed) {
			t.Fatalf("main markdown should not contain themed color %q: %q", themed, rendered)
		}
	}
	plain := stripAnsi(rendered)
	if !strings.Contains(plain, "Title") || !strings.Contains(plain, "link") || !strings.Contains(plain, "code") {
		t.Fatalf("main markdown missing expected content: %q", plain)
	}
}

func TestRenderMarkdownThinkingKeepsThinkingPalette(t *testing.T) {
	invalidateMarkdownCache()
	rendered := renderMarkdownThinking("# Title\n\n`code`", 80)
	for _, themed := range []string{"38;2;91;100;153", "38;2;107;115;148", "48;2;19;20;28"} {
		if strings.Contains(rendered, themed) {
			return
		}
	}
	t.Fatalf("thinking markdown should retain thinking palette: %q", rendered)
}
