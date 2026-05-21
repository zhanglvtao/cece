package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const ConfirmID = "confirm-tools"

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

// ToolCallInfo describes a pending tool call for display.
type ToolCallInfo struct {
	Name string
	Args string // short argument summary
}

var _ Dialog = (*Confirm)(nil)

// NewConfirm creates a new tool confirmation dialog.
func NewConfirm(styles DialogStyles, calls []ToolCallInfo) *Confirm {
	c := &Confirm{
		styles: styles,
		calls:  calls,
	}
	c.help = help.New()
	return c
}

// ID implements Dialog.
func (c *Confirm) ID() string { return ConfirmID }

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

// Draw implements Dialog.
func (c *Confirm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.View.GetHorizontalBorderSize()))

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

	DrawCenter(scr, area, view)
	return nil
}

// ShortHelp implements help.KeyMap.
func (c *Confirm) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "allow")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "deny")),
	}
}

// FullHelp implements help.KeyMap.
func (c *Confirm) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.ShortHelp()}
}
