package ui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// mdRendererCache caches glamour TermRenderers by width.
var (
	mdCacheMu sync.Mutex
	mdCache   = map[int]*glamour.TermRenderer{}
)

// renderMarkdown renders text as Markdown using glamour with a
// minimal monochrome style. On any glamour error the plain text
// is returned.
func renderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}
	r := getMarkdownRenderer(width)
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}

// getMarkdownRenderer returns a cached glamour TermRenderer for the
// given width.
func getMarkdownRenderer(width int) *glamour.TermRenderer {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	if r, ok := mdCache[width]; ok {
		return r
	}
	sty := buildGlamourStyle()
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(sty),
		glamour.WithWordWrap(width),
	)
	if r != nil {
		mdCache[width] = r
	}
	return r
}

// invalidateMarkdownCache drops all cached renderers.
func invalidateMarkdownCache() {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	mdCache = map[int]*glamour.TermRenderer{}
}

// buildGlamourStyle creates a monochrome ansi.StyleConfig —
// no decorative colors, only bold/italic for structure.
func buildGlamourStyle() ansi.StyleConfig {

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Italic: boolPtr(true),
			},
			Indent:      uintPtr(1),
			IndentToken: strPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{},
			LevelIndent: 4,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ",
				Bold:   boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Bold:   boolPtr(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Bold:   boolPtr(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Bold:   boolPtr(false),
			},
		},
		Text:          ansi.StylePrimitive{},
		Strikethrough: ansi.StylePrimitive{CrossedOut: boolPtr(true)},
		Emph:          ansi.StylePrimitive{Italic: boolPtr(true)},
		Strong:        ansi.StylePrimitive{Bold: boolPtr(true)},
		HorizontalRule: ansi.StylePrimitive{
			Format: "\n──────\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Format: "Image: {{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
								Prefix:          "\u00a0",
				Suffix:          "\u00a0",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				Margin: uintPtr(2),
			},
			Chroma: &ansi.Chroma{
				Text:                ansi.StylePrimitive{},
				Error:               ansi.StylePrimitive{},
				Comment:             ansi.StylePrimitive{Italic: boolPtr(true), Faint: boolPtr(true)},
				CommentPreproc:      ansi.StylePrimitive{Bold: boolPtr(true)},
				Keyword:             ansi.StylePrimitive{Bold: boolPtr(true)},
				KeywordReserved:     ansi.StylePrimitive{Bold: boolPtr(true)},
				KeywordNamespace:    ansi.StylePrimitive{Bold: boolPtr(true)},
				KeywordType:         ansi.StylePrimitive{Italic: boolPtr(true)},
				Operator:            ansi.StylePrimitive{},
				Punctuation:         ansi.StylePrimitive{},
				Name:                ansi.StylePrimitive{},
				NameBuiltin:         ansi.StylePrimitive{},
				NameTag:             ansi.StylePrimitive{Bold: boolPtr(true)},
				NameAttribute:       ansi.StylePrimitive{},
				NameClass:           ansi.StylePrimitive{Bold: boolPtr(true), Underline: boolPtr(true)},
				NameConstant:        ansi.StylePrimitive{},
				NameDecorator:       ansi.StylePrimitive{Bold: boolPtr(true)},
				NameException:       ansi.StylePrimitive{Bold: boolPtr(true)},
				NameFunction:        ansi.StylePrimitive{Bold: boolPtr(true)},
				NameOther:           ansi.StylePrimitive{},
				Literal:             ansi.StylePrimitive{},
				LiteralNumber:       ansi.StylePrimitive{},
				LiteralDate:         ansi.StylePrimitive{},
				LiteralString:       ansi.StylePrimitive{},
				LiteralStringEscape: ansi.StylePrimitive{},
				GenericDeleted:      ansi.StylePrimitive{CrossedOut: boolPtr(true)},
				GenericEmph:         ansi.StylePrimitive{Italic: boolPtr(true)},
				GenericInserted:     ansi.StylePrimitive{Bold: boolPtr(true)},
				GenericStrong:       ansi.StylePrimitive{Bold: boolPtr(true)},
				GenericSubheading:   ansi.StylePrimitive{Italic: boolPtr(true)},
							},
		},
		Table:          ansi.StyleTable{},
		DefinitionList: ansi.StyleBlock{},
		DefinitionTerm: ansi.StylePrimitive{},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n🠶 ",
		},
		HTMLBlock: ansi.StyleBlock{},
		HTMLSpan:  ansi.StyleBlock{},
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }
