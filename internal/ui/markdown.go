package ui

import (
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
)

var (
	mdCacheMu sync.Mutex
	mdCache   = map[int]*glamour.TermRenderer{}

	// mdBgColor is the background color used by the Glamour markdown renderer.
	// Empty string means no background (transparent).
	mdBgColor = ""
)

// SetMarkdownBackground sets the background color for markdown rendering.
// Must be called before any rendering occurs.
func SetMarkdownBackground(hex string) {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	mdBgColor = hex
	// Clear cache since background color changed
	mdCache = map[int]*glamour.TermRenderer{}
}

func markdownRenderer(width int) *glamour.TermRenderer {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	if r, ok := mdCache[width]; ok {
		return r
	}
	r, _ := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyleConfig()),
		glamour.WithWordWrap(width),
	)
	mdCache[width] = r
	return r
}

func markdownStyleConfig() ansi.StyleConfig {
	docPrim := ansi.StylePrimitive{
		Color: stringPtr("#c9d1d9"),
	}
	if mdBgColor != "" {
		docPrim.BackgroundColor = stringPtr(mdBgColor)
	}
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: docPrim,
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Indent:         uintPtr(1),
			IndentToken:    strPtr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: 4,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       stringPtr("#79c0ff"),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: " ",
				Suffix: " ",
				Color:  stringPtr("#1f6feb"),
				Bold:   boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
			},
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Link: ansi.StylePrimitive{
			Color:     stringPtr("#58a6ff"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr("#7ee787"),
			Bold:  boolPtr(true),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           stringPtr("#ff7b72"),
				BackgroundColor: stringPtr("#2d333b"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr("#8b949e"),
				},
				Margin: uintPtr(2),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: stringPtr("#c9d1d9"),
				},
				Error: ansi.StylePrimitive{
					Color:           stringPtr("#f0f6fc"),
					BackgroundColor: stringPtr("#f85149"),
				},
				Comment: ansi.StylePrimitive{
					Color: stringPtr("#8b949e"),
				},
				Keyword: ansi.StylePrimitive{
					Color: stringPtr("#ff7b72"),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: stringPtr("#ff7b72"),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: stringPtr("#ff7b72"),
				},
				KeywordType: ansi.StylePrimitive{
					Color: stringPtr("#79c0ff"),
				},
				Operator: ansi.StylePrimitive{
					Color: stringPtr("#79c0ff"),
				},
				Punctuation: ansi.StylePrimitive{
					Color: stringPtr("#c9d1d9"),
				},
				Name: ansi.StylePrimitive{
					Color: stringPtr("#c9d1d9"),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: stringPtr("#ffa657"),
				},
				NameTag: ansi.StylePrimitive{
					Color: stringPtr("#7ee787"),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: stringPtr("#79c0ff"),
				},
				NameClass: ansi.StylePrimitive{
					Color:     stringPtr("#ffa657"),
					Underline: boolPtr(true),
					Bold:      boolPtr(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: stringPtr("#ffa657"),
				},
				NameFunction: ansi.StylePrimitive{
					Color: stringPtr("#d2a8ff"),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: stringPtr("#79c0ff"),
				},
				LiteralString: ansi.StylePrimitive{
					Color: stringPtr("#a5d6ff"),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: stringPtr("#a5d6ff"),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: stringPtr("#ffa198"),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: stringPtr("#7ee787"),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: stringPtr("#8b949e"),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: stringPtr("#161b22"),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n ",
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
	}
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }
func strPtr(s string) *string    { return &s }

// renderMarkdown renders content as markdown with the given width.
func renderMarkdown(content string, width int) string {
	if content == "" {
		return ""
	}
	renderer := markdownRenderer(width)
	out, err := renderer.Render(content)
	if err != nil {
		return content
	}
	// Trim trailing newline that glamour adds
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}
