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
	Magenta = ansi.Magenta // sub-agent indicator
	Blue    = ansi.Blue   // plan / info
	Yellow  = ansi.Yellow // tool calls / warnings
	Green   = ansi.Green  // success / selected
	Red     = ansi.Red    // errors / destructive

	// Markdown rendering colors — Modern Dark palette.
	// High-luminance accent colors on dark background for maximum readability.
	MdHeading  = "#7dcfff" // Bright cyan — headings (bold)
	MdLink     = "#82aaff" // Soft blue — link URLs
	MdCode     = "#c3e88d" // Light green — inline code
	MdCodeBg   = "#1a1b26" // Deep navy — code block background
	MdMuted    = "#565f89" // Muted blue-gray — dimmed elements
	MdKeyword  = "#bb9af7" // Lavender — syntax keywords
	MdString   = "#e0af68" // Gold — string literals
	MdNumber   = "#7aa2f7" // Blue — number literals
	MdDeleted  = "#f7768e" // Coral red — deleted text
	MdInserted = "#9ece6a" // Yellow-green — inserted text

	// Thinking block colors — subdued palette that clearly distinguishes
	// thinking content from the assistant's main output.
	MdThinkHeading  = "#5b6499" // Dimmed blue-gray — thinking headings
	MdThinkLink     = "#5b6499" // Dimmed blue-gray — thinking links
	MdThinkCode     = "#6b7394" // Muted blue — thinking inline code
	MdThinkCodeBg   = "#13141c" // Darker navy — thinking code block bg
	MdThinkMuted    = "#3b3f5c" // Deep muted — thinking dimmed elements
	MdThinkKeyword  = "#7b7fa8" // Faded lavender — thinking keywords
	MdThinkString   = "#8b7a4a" // Faded gold — thinking strings
	MdThinkNumber   = "#5b6499" // Dimmed blue — thinking numbers
	MdThinkDeleted  = "#7b4a52" // Faded coral — thinking deleted
	MdThinkInserted = "#5a7a4a" // Faded green — thinking inserted
)
