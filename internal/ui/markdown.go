package ui

import (
	"strings"
	"sync"

	"cece/internal/ui/theme"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

// mdRendererCache caches glamour TermRenderers by width.
// glamour's TermRenderer is stateful (goldmark BlockStack) so each
// width gets its own instance; callers must serialize access when
// used concurrently (the TUI Update loop is single-threaded so this
// is fine in production).
var (
	mdCacheMu sync.Mutex
	mdCache   = map[int]*glamour.TermRenderer{}
)

// renderMarkdown renders text as Markdown using glamour, with a style
// derived from the given palette. The output is trimmed of surrounding
// whitespace. On any glamour error the plain text is returned.
func renderMarkdown(text string, width int, p theme.Palette) string {
	if text == "" {
		return ""
	}
	r := getMarkdownRenderer(width, p)
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}

// getMarkdownRenderer returns a cached glamour TermRenderer for the
// given width. Renderers are reused across calls; call
// invalidateMarkdownCache when the palette changes.
func getMarkdownRenderer(width int, p theme.Palette) *glamour.TermRenderer {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	if r, ok := mdCache[width]; ok {
		return r
	}
	sty := buildGlamourStyle(p)
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(sty),
		glamour.WithWordWrap(width),
	)
	if r != nil {
		mdCache[width] = r
	}
	return r
}

// invalidateMarkdownCache drops all cached renderers so the next
// call picks up a fresh palette.
func invalidateMarkdownCache() {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	mdCache = map[int]*glamour.TermRenderer{}
}

// buildGlamourStyle creates an ansi.StyleConfig aligned with
// cece's theme.Palette. Keeps the minimal set of overrides —
// glamour's default structure (prefixes, block layout) is
// preserved; only colors are swapped.
func buildGlamourStyle(p theme.Palette) ansi.StyleConfig {
	base := p.FgBase
	subtle := p.FgSubtle
	muted := p.FgMuted
	bg := p.BgFaint

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr(theme.HexColor(base)),
			},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr(theme.HexColor(subtle)),
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
				Color:        strPtr(theme.HexColor(p.Primary)),
				Bold:         boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ",
				Color:  strPtr(theme.HexColor(p.Primary)),
				Bold:   boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  strPtr(theme.HexColor(p.InfoMuted)),
				Bold:   boolPtr(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  strPtr(theme.HexColor(p.Accent)),
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
				Color:  strPtr(theme.HexColor(muted)),
				Bold:   boolPtr(false),
			},
		},
		Text:          ansi.StylePrimitive{},
		Strikethrough: ansi.StylePrimitive{CrossedOut: boolPtr(true)},
		Emph:          ansi.StylePrimitive{Italic: boolPtr(true)},
		Strong:        ansi.StylePrimitive{Bold: boolPtr(true)},
		HorizontalRule: ansi.StylePrimitive{
			Color:  strPtr(theme.HexColor(p.Separator)),
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
			Color:     strPtr(theme.HexColor(p.Info)),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     strPtr(theme.HexColor(p.Secondary)),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Format: "Image: {{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           strPtr(theme.HexColor(p.Warning)),
				BackgroundColor: strPtr(theme.HexColor(bg)),
				Prefix:          "\u00a0",
				Suffix:          "\u00a0",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(subtle)),
				},
				Margin: uintPtr(2),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(base)),
				},
				Error: ansi.StylePrimitive{
					Color:           strPtr("#F1F1F1"),
					BackgroundColor: strPtr(theme.HexColor(p.Destructive)),
				},
				Comment: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(muted)),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Warning)),
				},
				Keyword: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Primary)),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Secondary)),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Secondary)),
				},
				KeywordType: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Accent)),
				},
				Operator: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Destructive)),
				},
				Punctuation: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(subtle)),
				},
				Name: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(base)),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Warning)),
				},
				NameTag: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.InfoMuted)),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Primary)),
				},
				NameClass: ansi.StylePrimitive{
					Color:      strPtr(theme.HexColor(base)),
					Underline:  boolPtr(true),
					Bold:       boolPtr(true),
				},
				NameConstant:   ansi.StylePrimitive{},
				NameDecorator: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Warning)),
				},
				NameException:  ansi.StylePrimitive{},
				NameFunction: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.SuccessMuted)),
				},
				NameOther:      ansi.StylePrimitive{},
				Literal:        ansi.StylePrimitive{},
				LiteralNumber: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Success)),
				},
				LiteralDate:    ansi.StylePrimitive{},
				LiteralString: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.WarningMuted)),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Success)),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Destructive)),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(p.Success)),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: strPtr(theme.HexColor(muted)),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: strPtr(theme.HexColor(bg)),
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
func boolPtr(b bool) *bool     { return &b }
func uintPtr(u uint) *uint     { return &u }
