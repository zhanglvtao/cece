package theme

import "github.com/charmbracelet/x/ansi"

// ANSI 16-color constants — automatically follow terminal color scheme.
// These map to the terminal's palette, so switching Ghostty/iTerm themes
// changes cece's colors automatically.
const (
	// Foreground hierarchy.
	Fg       = ansi.BrightWhite // default text
	FgSubtle = ansi.White       // secondary text
	FgMuted  = ansi.BrightBlack // dimmed/decorative text

	// Semantic colors.
	Primary = ansi.Cyan   // brand / prompt / active indicator
	Blue    = ansi.Blue   // plan / info
	Yellow  = ansi.Yellow // tool calls / warnings
	Green   = ansi.Green  // success / selected
	Red     = ansi.Red    // errors / destructive

	// Markdown rendering colors — VIM-inspired palette.
	// Classic dark-terminal colors with warm tones and high readability.
	MdHeading  = "#ffff60" // Bright yellow — headings
	MdLink     = "#569cd6" // Steel blue — link URLs
	MdCode     = "#4ec9b0" // Teal — inline code
	MdCodeBg   = "#1e1e1e" // Near-black — code block background
	MdMuted    = "#6a9955" // Olive green — dimmed elements
	MdKeyword  = "#c586c0" // Lavender — syntax keywords
	MdString   = "#ce9178" // Orange-brown — string literals
	MdNumber   = "#b5cea8" // Light green — number literals
	MdDeleted  = "#f44747" // Red — deleted text
	MdInserted = "#4ec9b0" // Teal — inserted text
)
