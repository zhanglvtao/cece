package theme

import (
	"fmt"
	"image/color"
)

// HexColor converts a color.Color to a hex string like "#FF60FF" for use
// with lipgloss.Color. This is needed because lipgloss.Style.ForegroundColor
// and BackgroundColor accept color.Color, but some lipgloss APIs (like
// lipgloss.Color()) work best with hex strings.
func HexColor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}
