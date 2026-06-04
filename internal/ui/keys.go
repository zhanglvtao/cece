package ui

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap holds all keyboard bindings for the TUI.
type KeyMap struct {
	Quit   key.Binding
	Help   key.Binding
	Cancel key.Binding

	Editor struct {
		Send        key.Binding
		Newline     key.Binding
		HistoryUp   key.Binding
		HistoryDown key.Binding
		Complete    key.Binding
	}

	Chat struct {
		Up       key.Binding
		Down     key.Binding
		PageUp   key.Binding
		PageDown key.Binding
		Home     key.Binding
		End      key.Binding
		Expand   key.Binding
	}

	SwitchFocus    key.Binding
	TogglePlanMode key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	var k KeyMap

	k.Quit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "clear/quit (2x to force)"),
	)
	k.Help = key.NewBinding(
		key.WithKeys("ctrl+g"),
		key.WithHelp("ctrl+g", "help"),
	)
	k.Cancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)

	k.Editor.Send = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "send"),
	)
	k.Editor.Newline = key.NewBinding(
		key.WithKeys("ctrl+j", "shift+enter"),
		key.WithHelp("ctrl+j", "newline"),
	)
	k.Editor.HistoryUp = key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "prev"),
	)
	k.Editor.HistoryDown = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next"),
	)
	k.Editor.Complete = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "complete"),
	)

	k.Chat.Up = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	)
	k.Chat.Down = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	)
	k.Chat.PageUp = key.NewBinding(
		key.WithKeys("pgup", "b"),
		key.WithHelp("b", "page up"),
	)
	k.Chat.PageDown = key.NewBinding(
		key.WithKeys("pgdown", "f"),
		key.WithHelp("f", "page down"),
	)
	k.Chat.Home = key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "top"),
	)
	k.Chat.End = key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "bottom"),
	)
	k.Chat.Expand = key.NewBinding(
		key.WithKeys("space", "enter"),
		key.WithHelp("space", "expand"),
	)

	k.SwitchFocus = key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "switch focus"),
	)
	k.TogglePlanMode = key.NewBinding(
		key.WithKeys("shift+tab", "backtab"),
		key.WithHelp("shift+tab", "cycle mode"),
	)

	return k
}
