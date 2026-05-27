package anim

import (
	"image/color"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// DrawChatBorderBusy draws a thick line border (━┃┏┓┗┛) around the chat area
// with the given breathing gradient color.
func DrawChatBorderBusy(scr uv.Screen, area uv.Rectangle, borderColor color.Color, logo string, logoStyle lipgloss.Style) {
	border := uv.ThickBorder().Style(uv.Style{
		Fg: borderColor,
	})
	border.Draw(scr, area)
	drawTopLogo(scr, area, logo, logoStyle)
}

// DrawChatBorderIdle draws a thin line border (─│┌┐└┘) around the chat area
// in the static separator color.
func DrawChatBorderIdle(scr uv.Screen, area uv.Rectangle, separatorColor color.Color, logo string, logoStyle lipgloss.Style) {
	border := uv.NormalBorder().Style(uv.Style{
		Fg: separatorColor,
	})
	border.Draw(scr, area)
	drawTopLogo(scr, area, logo, logoStyle)
}

func drawTopLogo(scr uv.Screen, area uv.Rectangle, logo string, logoStyle lipgloss.Style) {
	logo = logoStyle.Render(logo)
	logoWidth := lipgloss.Width(logo)
	if logoWidth == 0 || area.Dx() < logoWidth+4 || area.Dy() < 1 {
		return
	}
	x := area.Min.X + 2
	uv.NewStyledString(logo).Draw(scr, uv.Rect(x, area.Min.Y, logoWidth, 1))
}
