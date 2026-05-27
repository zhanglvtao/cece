package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const (
	ConfirmID     = "confirm-tools"
	ExitConfirmID = "confirm-exit"
)

// ActionConfirmTools signals that the user approved tool execution.
type ActionConfirmTools struct{}

// ActionRejectTools signals that the user rejected tool execution.
type ActionRejectTools struct{}

// Confirm is a dialog that asks the user to approve tool calls.
type Confirm struct {
	styles DialogStyles
	help   help.Model
	calls  []ToolCallInfo
}

// ExitConfirm is a dialog that asks whether the current session should be saved before quitting.
type ExitConfirm struct {
	styles      DialogStyles
	help        help.Model
	hasSession  bool
	willDiscard bool
}

// ToolCallInfo describes a pending tool call for display.
type ToolCallInfo struct {
	Name string
	Args string // short argument summary
}

var _ Dialog = (*Confirm)(nil)
var _ Dialog = (*ExitConfirm)(nil)

// NewConfirm creates a new tool confirmation dialog.
func NewConfirm(styles DialogStyles, calls []ToolCallInfo) *Confirm {
	c := &Confirm{
		styles: styles,
		calls:  calls,
	}
	c.help = help.New()
	return c
}

// NewExitConfirm creates a new exit confirmation dialog.
func NewExitConfirm(styles DialogStyles, hasSession bool, willDiscard bool) *ExitConfirm {
	d := &ExitConfirm{
		styles:      styles,
		hasSession:  hasSession,
		willDiscard: willDiscard,
	}
	d.help = help.New()
	return d
}

// ID implements Dialog.
func (c *Confirm) ID() string { return ConfirmID }

// DesiredHeight implements Dialog.
func (c *Confirm) DesiredHeight() int { return 10 }

// ID implements Dialog.
func (c *ExitConfirm) ID() string { return ExitConfirmID }

// DesiredHeight implements Dialog.
func (c *ExitConfirm) DesiredHeight() int { return 10 }

// HandleMsg implements Dialog.
func (c *Confirm) HandleMsg(msg tea.Msg) Action {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(kp, key.NewBinding(key.WithKeys("enter", "y"))):
		return ActionConfirmTools{}
	case key.Matches(kp, CloseKey), key.Matches(kp, key.NewBinding(key.WithKeys("n"))):
		return ActionRejectTools{}
	}
	return nil
}

// HandleMsg implements Dialog.
func (c *ExitConfirm) HandleMsg(msg tea.Msg) Action {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(kp, key.NewBinding(key.WithKeys("enter", "y"))):
		return ActionSaveSessionAndQuit{}
	case key.Matches(kp, key.NewBinding(key.WithKeys("n"))):
		return ActionDiscardSessionAndQuit{}
	case key.Matches(kp, CloseKey):
		return ActionClose{}
	}
	return nil
}

// Draw implements Dialog.
func (c *Confirm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.styles
	width := max(0, area.Dx()-t.View.GetHorizontalBorderSize())

	rc := NewRenderContext(t.Title, t.View, width)
	rc.Title = "Allow tool calls?"
	rc.Gap = 1

	// Tool call items wrapped in a content panel.
	var lines []string
	for _, call := range c.calls {
		line := fmt.Sprintf("  ▸ %s", call.Name)
		if call.Args != "" {
			argPreview := call.Args
			if len(argPreview) > 40 {
				argPreview = argPreview[:37] + "..."
			}
			line += fmt.Sprintf("  %s", t.InfoBlurred.Render(argPreview))
		}
		lines = append(lines, line)
	}
	panelContent := strings.Join(lines, "\n")
	rc.AddPart(t.ContentPanel.Width(width - t.View.GetHorizontalFrameSize()).Render(panelContent))

	// Styled action buttons.
	allowBtn := t.AllowBtn.Render("[y] allow")
	denyBtn := t.DenyBtn.Render("[n] deny")
	rc.Help = fmt.Sprintf("%s  %s", allowBtn, denyBtn)

	view := rc.Render()

	DrawInline(scr, area, view, nil)
	return nil
}

// Draw implements Dialog.
func (c *ExitConfirm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.styles
	width := max(0, area.Dx()-t.View.GetHorizontalBorderSize())

	rc := NewRenderContext(t.Title, t.View, width)
	rc.Title = "Quit cece?"
	rc.Gap = 1

	message := "Save current session before quitting?"
	if !c.hasSession {
		message = "Quit cece?"
	}
	lines := []string{"  " + message}
	if c.willDiscard {
		lines = append(lines, "  "+t.DeletingMessage.Render("Choose [n] to delete current session storage and quit."))
	} else if c.hasSession {
		lines = append(lines, "  "+t.InfoBlurred.Render("Choose [n] to quit without saving this session."))
	}
	lines = append(lines, "  "+t.InfoBlurred.Render("Press [esc] to cancel."))
	panelContent := strings.Join(lines, "\n")
	rc.AddPart(t.ContentPanel.Width(width - t.View.GetHorizontalFrameSize()).Render(panelContent))

	saveBtn := t.AllowBtn.Render("[y] save & quit")
	discardLabel := "[n] quit"
	if c.willDiscard {
		discardLabel = "[n] don't save"
	}
	discardBtn := t.DenyBtn.Render(discardLabel)
	rc.Help = fmt.Sprintf("%s  %s  [esc] cancel", saveBtn, discardBtn)

	view := rc.Render()
	DrawInline(scr, area, view, nil)
	return nil
}

// ShortHelp implements help.KeyMap.
func (c *Confirm) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "allow")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "deny")),
	}
}

// ShortHelp implements help.KeyMap.
func (c *ExitConfirm) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "save and quit")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "quit without saving")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// FullHelp implements help.KeyMap.
func (c *Confirm) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.ShortHelp()}
}

// FullHelp implements help.KeyMap.
func (c *ExitConfirm) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.ShortHelp()}
}
