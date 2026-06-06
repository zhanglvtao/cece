package ui

import (
	"strings"
	"sync"

	"github.com/zhanglvtao/cece/internal/ui/theme"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// mdRendererCache caches glamour TermRenderers by width.
var (
	mdCacheMu sync.Mutex
	mdCache   = map[int]*glamour.TermRenderer{}
)

// renderMarkdown renders text as Markdown using glamour with ANSI 16-color
// styling that follows the terminal theme. On any glamour error the plain
// text is returned.
func renderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}
	r := getMarkdownRenderer(width)
	rendererMu.Lock()
	out, err := r.Render(text)
	rendererMu.Unlock()
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

// buildGlamourStyle creates an ANSI 16-color ansi.StyleConfig.
// Colors reference the terminal palette so switching themes
// changes cece's markdown colors automatically.
func buildGlamourStyle() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Italic: boolPtr(true),
				Color:  strPtr(theme.MdMuted),
			},
			Indent:      uintPtr(1),
			IndentToken: strPtr("│ "),
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			StyleBlock:   ansi.StyleBlock{},
			LevelIndent:  4,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       strPtr(theme.MdHeading),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: " ",
				Suffix: " ",
				Color:  strPtr(theme.MdHeading),
				Bold:   boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  strPtr(theme.MdHeading),
				Bold:   boolPtr(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  strPtr(theme.MdHeading),
				Bold:   boolPtr(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  strPtr(theme.MdHeading),
				Bold:   boolPtr(true),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  strPtr(theme.MdHeading),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  strPtr(theme.MdMuted),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  strPtr(theme.MdMuted),
			Format: "\n--------\n",
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
			Color:     strPtr(theme.MdLink),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr(theme.MdLink),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     strPtr(theme.MdLink),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Format: "Image: {{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr(theme.MdCode),
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
				Error:               ansi.StylePrimitive{Color: strPtr(theme.MdDeleted)},
				Comment:             ansi.StylePrimitive{Italic: boolPtr(true), Faint: boolPtr(true), Color: strPtr(theme.MdMuted)},
				CommentPreproc:      ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				Keyword:             ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				KeywordReserved:     ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				KeywordNamespace:    ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				KeywordType:         ansi.StylePrimitive{Italic: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				Operator:            ansi.StylePrimitive{},
				Punctuation:         ansi.StylePrimitive{},
				Name:                ansi.StylePrimitive{},
				NameBuiltin:         ansi.StylePrimitive{},
				NameTag:             ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				NameAttribute:       ansi.StylePrimitive{},
				NameClass:           ansi.StylePrimitive{Bold: boolPtr(true), Underline: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				NameConstant:        ansi.StylePrimitive{},
				NameDecorator:       ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				NameException:       ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdDeleted)},
				NameFunction:        ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdKeyword)},
				NameOther:           ansi.StylePrimitive{},
				Literal:             ansi.StylePrimitive{},
				LiteralNumber:       ansi.StylePrimitive{Color: strPtr(theme.MdNumber)},
				LiteralDate:         ansi.StylePrimitive{},
				LiteralString:       ansi.StylePrimitive{Color: strPtr(theme.MdString)},
				LiteralStringEscape: ansi.StylePrimitive{Color: strPtr(theme.MdInserted)},
				GenericDeleted:      ansi.StylePrimitive{CrossedOut: boolPtr(true), Color: strPtr(theme.MdDeleted)},
				GenericEmph:         ansi.StylePrimitive{Italic: boolPtr(true)},
				GenericInserted:     ansi.StylePrimitive{Bold: boolPtr(true), Color: strPtr(theme.MdInserted)},
				GenericStrong:       ansi.StylePrimitive{Bold: boolPtr(true)},
				GenericSubheading:   ansi.StylePrimitive{Italic: boolPtr(true), Color: strPtr(theme.MdMuted)},
				Background: ansi.StylePrimitive{
					BackgroundColor: strPtr(theme.MdCodeBg),
				},
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
