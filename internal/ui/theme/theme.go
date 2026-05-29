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

	// Markdown rendering colors (ANSI 16-color index strings).
	// These follow the terminal theme automatically.
	MdHeading  = "6" // Cyan — headings, links
	MdLink     = "6" // Cyan — link URLs
	MdCode     = "3" // Yellow — inline code
	MdCodeBg   = "0" // Black — code block background
	MdMuted    = "8" // BrightBlack — dimmed elements
	MdKeyword  = "6" // Cyan — syntax keywords
	MdString   = "2" // Green — string literals
	MdNumber   = "3" // Yellow — number literals
	MdDeleted  = "1" // Red — deleted text
	MdInserted = "2" // Green — inserted text
)
