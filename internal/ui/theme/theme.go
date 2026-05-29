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
)
