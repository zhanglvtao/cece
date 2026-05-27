package dialog

import (
	"cece/internal/ui/theme"

	"charm.land/lipgloss/v2"
)

// DialogStyles holds all style definitions for dialog rendering.
type DialogStyles struct {
	View        lipgloss.Style
	Title       lipgloss.Style
	InputPrompt lipgloss.Style
	HelpView    lipgloss.Style
	ListStyle   lipgloss.Style

	// Session item styles
	NormalItem          lipgloss.Style
	SelectedItem        lipgloss.Style
	InfoBlurred         lipgloss.Style
	InfoFocused         lipgloss.Style
	DeletingMessage     lipgloss.Style
	RenamingMessage     lipgloss.Style
	DeletingItemBlurred lipgloss.Style
	DeletingItemFocused lipgloss.Style
	RenamingItemBlurred lipgloss.Style
	RenamingItemFocused lipgloss.Style

	// Modern additions
	ContentPanel  lipgloss.Style
	AllowBtn      lipgloss.Style
	DenyBtn       lipgloss.Style

	// Scrollbar
	ScrollbarThumb lipgloss.Style
	ScrollbarTrack lipgloss.Style
}

// DefaultDialogStyles returns the default dialog styles built from the theme palette.
func DefaultDialogStyles() DialogStyles {
	return BuildDialogStyles(theme.DefaultPalette())
}

// BuildDialogStyles constructs a DialogStyles from a theme.Palette.
func BuildDialogStyles(p theme.Palette) DialogStyles {
	var (
		base = lipgloss.NewStyle().Foreground(p.FgBase)
	)

	return DialogStyles{
		View: base.Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Primary).
			Padding(1, 2),
		Title: base.Bold(true).Foreground(p.Primary),
		InputPrompt: base.
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(p.Primary),
		HelpView:  base,
		ListStyle: lipgloss.NewStyle(),
		NormalItem: base.Foreground(p.FgMuted).Padding(0, 1),
		SelectedItem: base.
			Foreground(p.FgBase).
			Padding(0, 1),
		InfoBlurred: base.Foreground(p.FgMuted),
		InfoFocused: base.Foreground(p.FgBase),
		DeletingMessage: base,
		RenamingMessage: base,

		DeletingItemBlurred: base.Foreground(p.FgMuted).Padding(0, 1),
		DeletingItemFocused: base.
			Foreground(p.FgBase).
			Padding(0, 1),
		RenamingItemBlurred: base.Foreground(p.FgMuted).Padding(0, 1),
		RenamingItemFocused: base.
			Foreground(p.FgBase).
			Padding(0, 1),

		ContentPanel: base.
			Padding(0, 2),

		AllowBtn: base.
			Bold(true).
			Padding(0, 1),
		DenyBtn: base.
			Padding(0, 1),

		ScrollbarThumb: base.Foreground(p.Secondary),
		ScrollbarTrack: base.Foreground(p.Separator),
	}
}
