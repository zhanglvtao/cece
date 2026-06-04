package testkit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// KeyMsg builds a tea.KeyPressMsg from a short keyboard description.
// Supported strings: single printable runes ("a"), named keys
// ("enter", "esc", "up", "down", "tab", "backspace", "space", "pgup",
// "pgdown", "home", "end") and modifier prefixes ("ctrl+x", "alt+x",
// "shift+x", "shift+tab"/"backtab").
func KeyMsg(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{
		Text: TextForKey(s),
		Code: CodeForKey(s),
		Mod:  ModForKey(s),
	})
}

// TextForKey returns the literal text inserted by a key, if any.
// Modifier-prefixed and named keys insert nothing.
func TextForKey(s string) string {
	if len([]rune(s)) == 1 {
		return s
	}
	return ""
}

// CodeForKey maps a short key description to a tea.Key code.
func CodeForKey(s string) rune {
	switch s {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "ctrl+up", "alt+up":
		return tea.KeyUp
	case "ctrl+down", "alt+down":
		return tea.KeyDown
	case "pgup":
		return tea.KeyPgUp
	case "pgdown":
		return tea.KeyPgDown
	case "home":
		return tea.KeyHome
	case "end":
		return tea.KeyEnd
	case "tab":
		return tea.KeyTab
	case "shift+tab", "backtab":
		return tea.KeyTab
	case "backspace":
		return tea.KeyBackspace
	case " ", "space":
		return tea.KeySpace
	}
	// Modifier-prefixed single rune (e.g. "ctrl+c", "alt+x").
	if i := strings.LastIndex(s, "+"); i >= 0 {
		tail := s[i+1:]
		if len([]rune(tail)) == 1 {
			return []rune(tail)[0]
		}
	}
	runes := []rune(s)
	if len(runes) == 1 {
		return runes[0]
	}
	return 0
}

// ModForKey returns the modifier mask for a key description prefixed
// with "ctrl+", "alt+" or "shift+".
func ModForKey(s string) tea.KeyMod {
	switch {
	case strings.HasPrefix(s, "ctrl+"):
		return tea.ModCtrl
	case strings.HasPrefix(s, "alt+"):
		return tea.ModAlt
	case strings.HasPrefix(s, "shift+"):
		return tea.ModShift
	default:
		return 0
	}
}

// TypeMsgs converts a string into a sequence of single-rune key presses.
// Useful for typing into a textarea.
func TypeMsgs(text string) []tea.Msg {
	out := make([]tea.Msg, 0, len(text))
	for _, r := range text {
		out = append(out, KeyMsg(string(r)))
	}
	return out
}
