package dialog

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// RenderContext is a dialog rendering context for common dialog layouts.
type RenderContext struct {
	TitleStyle lipgloss.Style
	ViewStyle  lipgloss.Style
	Width      int
	Gap        int
	Title      string
	TitleInfo  string
	Parts      []string
	Help       string
}

// NewRenderContext creates a new RenderContext with the provided styles and width.
func NewRenderContext(titleStyle, viewStyle lipgloss.Style, width int) *RenderContext {
	return &RenderContext{
		TitleStyle: titleStyle,
		ViewStyle:  viewStyle,
		Width:      width,
		Parts:      []string{},
	}
}

// AddPart adds a rendered part to the dialog.
func (rc *RenderContext) AddPart(part string) {
	if len(part) > 0 {
		rc.Parts = append(rc.Parts, part)
	}
}

// Render renders the dialog using the provided context.
func (rc *RenderContext) Render() string {
	dialogStyle := rc.ViewStyle.Width(rc.Width)

	var parts []string

	if len(rc.Title) > 0 {
		title := rc.Title
		if len(rc.TitleInfo) > 0 {
			title += rc.TitleInfo
		}
		parts = append(parts, rc.TitleStyle.Render(title))
		if rc.Gap > 0 {
			parts = append(parts, make([]string, rc.Gap)...)
		}
	}

	if rc.Gap <= 0 {
		parts = append(parts, rc.Parts...)
	} else {
		for i, p := range rc.Parts {
			if len(p) > 0 {
				parts = append(parts, p)
			}
			if i < len(rc.Parts)-1 {
				parts = append(parts, make([]string, rc.Gap)...)
			}
		}
	}

	if len(rc.Help) > 0 {
		if rc.Gap > 0 {
			parts = append(parts, make([]string, rc.Gap)...)
		}
		helpWidth := rc.Width - dialogStyle.GetHorizontalFrameSize()
		helpStyle := lipgloss.NewStyle().Width(helpWidth)
		helpView := ansi.Truncate(helpStyle.Render(rc.Help), helpWidth-1, "")
		parts = append(parts, helpView)
	}

	content := strings.Join(parts, "\n")
	return dialogStyle.Render(content)
}

// InputCursor adjusts the cursor position for an input field within a dialog.
func InputCursor(titleStyle, dialogStyle, inputStyle lipgloss.Style, cur *tea.Cursor) *tea.Cursor {
	if cur != nil {
		cur.X += inputStyle.GetBorderLeftSize() +
			inputStyle.GetMarginLeft() +
			inputStyle.GetPaddingLeft() +
			dialogStyle.GetBorderLeftSize() +
			dialogStyle.GetPaddingLeft() +
			dialogStyle.GetMarginLeft()
		cur.Y += titleStyle.GetVerticalFrameSize() +
			inputStyle.GetBorderTopSize() +
			inputStyle.GetMarginTop() +
			inputStyle.GetPaddingTop() +
			inputStyle.GetBorderBottomSize() +
			inputStyle.GetMarginBottom() +
			inputStyle.GetPaddingBottom() +
			dialogStyle.GetPaddingTop() +
			dialogStyle.GetMarginTop() +
			dialogStyle.GetBorderTopSize()
	}
	return cur
}

// ApplyForegroundGrad renders a string with horizontal gradient foreground.
func ApplyForegroundGrad(base lipgloss.Style, input string, color1, color2 color.Color) string {
	if input == "" {
		return ""
	}
	if len(input) == 1 {
		return base.Foreground(color1).Render(input)
	}
	var clusters []string
	gr := strings.NewReader(input)
	for gr.Len() > 0 {
		ch, _, _ := gr.ReadRune()
		clusters = append(clusters, string(ch))
	}
	ramp := lipgloss.Blend1D(len(clusters), color1, color2)
	for i, c := range ramp {
		clusters[i] = base.Foreground(c).Render(clusters[i])
	}
	return strings.Join(clusters, "")
}
