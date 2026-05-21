package theme

import (
	"fmt"
	"image/color"
)

// hex parses a "#RRGGBB" hex string into a color.RGBA.
func hex(s string) color.RGBA {
	var r, g, b uint8
	fmtS := s[1:]
	if len(fmtS) == 6 {
		_, _ = fmt.Sscanf(fmtS, "%02x%02x%02x", &r, &g, &b)
	}
	return color.RGBA{R: r, G: g, B: b, A: 0xFF}
}

// DefaultPalette returns the Tokyo Night based dark palette.
func DefaultPalette() Palette {
	return Palette{
		// Brand — blue/pink/cyan identity.
		Primary:   hex("#7aa2f7"), // blue
		Secondary: hex("#f7768e"), // red/pink
		Accent:    hex("#7dcfff"), // cyan
		Keyword:   hex("#e0af68"), // amber

		// Foreground — light-to-dark for text hierarchy.
		FgBase:   hex("#c0caf5"), // foreground
		FgSubtle: hex("#a9b1d6"), // palette white
		FgMuted:  hex("#787c99"), // comment gray (readable on dark)
		FgFaint:  hex("#565f89"), // dimmed text

		// Background — dark-to-light with blue undertone.
		BgBase:      hex("#1a1b26"), // base background
		BgSubtle:    hex("#1f2335"), // panel / surface
		BgFaint:     hex("#24283b"), // raised surface
		BgHighlight: hex("#292e42"), // highlight / focus
		BgNeutral:   hex("#2a2833"), // neutral warm gray-purple
		BgAssistant: hex("#0d1f17"), // dark green-tinted background for assistant messages

		OnPrimary: hex("#1a1b26"), // dark text on bright primary bg

		Separator: hex("#3b4261"), // visible separator

		// Destructive — red for errors and danger.
		Destructive:      hex("#f7768e"), // red
		DestructiveMuted: hex("#db4b4b"), // darker red

		// Success — green for confirmations and tool success.
		Success:      hex("#9ece6a"), // green
		SuccessMuted: hex("#73daca"), // teal
		SuccessFaint: hex("#41a6b5"), // dark teal

		// Warning — amber for caution.
		Warning:      hex("#e0af68"), // amber
		WarningMuted: hex("#ff9e64"), // orange

		// Info — blue for informational elements.
		Info:      hex("#7aa2f7"), // blue
		InfoMuted: hex("#7dcfff"), // cyan
		InfoFaint: hex("#2ac3de"), // dark cyan

		// Busy — cyan for loading/activity.
		Busy: hex("#7dcfff"), // cyan
	}
}
