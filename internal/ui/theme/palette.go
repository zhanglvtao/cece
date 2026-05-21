package theme

import "image/color"

// Palette is the semantic color palette used to build all UI styles.
// Every color role is named by purpose, not by value — swap the palette
// and the entire UI adapts.
type Palette struct {
	// Brand.
	Primary   color.Color
	Secondary color.Color
	Accent    color.Color
	Keyword   color.Color

	// Foreground hierarchy: Base > Subtle > Muted > Faint.
	FgBase   color.Color
	FgSubtle color.Color
	FgMuted  color.Color
	FgFaint  color.Color

	// Background hierarchy: Base > Subtle > Faint > Highlight > Neutral.
	BgBase      color.Color
	BgSubtle    color.Color
	BgFaint     color.Color
	BgHighlight color.Color
	BgNeutral   color.Color
	BgAssistant color.Color // light green tint for assistant messages

	// Contrast foreground for primary-colored backgrounds.
	OnPrimary color.Color

	// Separators and dividers.
	Separator color.Color

	// Status colors — each with a bright, muted, and faint variant.
	Destructive      color.Color
	DestructiveMuted color.Color

	Success      color.Color
	SuccessMuted color.Color
	SuccessFaint color.Color

	Warning      color.Color
	WarningMuted color.Color

	Info      color.Color
	InfoMuted color.Color
	InfoFaint color.Color

	// Busy/active indicator.
	Busy color.Color
}
