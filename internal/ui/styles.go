package ui

import (
	"cece/internal/ui/theme"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Styles holds all TUI style definitions built from ANSI terminal colors.
type Styles struct {
	Chat struct {
		LabelUser      lipgloss.Style
		LabelAssistant lipgloss.Style
		LabelThinking  lipgloss.Style
		LabelTool      lipgloss.Style
		LabelError     lipgloss.Style
		LabelSystem    lipgloss.Style
		LabelPlan      lipgloss.Style
		LabelInfo      lipgloss.Style
	}
	Input struct {
		Textarea textarea.Styles
		Box      lipgloss.Style
		BoxBusy  lipgloss.Style
		BoxIdle  lipgloss.Style
	}
	Modal struct {
		Title   lipgloss.Style
		Help    lipgloss.Style
		Cursor  lipgloss.Style
		Tool    lipgloss.Style
		ToolArg lipgloss.Style
	}
	Picker struct {
		Title   lipgloss.Style
		Cursor  lipgloss.Style
		Item    lipgloss.Style // non-selected item text
		Help    lipgloss.Style
		Filter  lipgloss.Style
		Command lipgloss.Style
		Info    lipgloss.Style // secondary info: model, time
		Preview lipgloss.Style // preview text below title
	}
	Headline lipgloss.Style
	Queued   lipgloss.Style
	Status   struct {
		Separator lipgloss.Style
		Model     lipgloss.Style
		Context   lipgloss.Style
		Tokens    lipgloss.Style
		Calls     lipgloss.Style
		Tool      lipgloss.Style
		Scroll    lipgloss.Style
	}
	Task struct {
		Label      lipgloss.Style
		Pending    lipgloss.Style
		InProgress lipgloss.Style
		Completed  lipgloss.Style
	}
	Agent struct {
		Label   lipgloss.Style
		Running lipgloss.Style
	}
}

// DefaultStyles returns the style set built from ANSI terminal colors.
func DefaultStyles() Styles {
	var s Styles

	s.Chat.LabelUser = lipgloss.NewStyle().Foreground(theme.Fg)
	s.Chat.LabelAssistant = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Chat.LabelThinking = lipgloss.NewStyle().Foreground(theme.FgMuted).Italic(true)
	s.Chat.LabelTool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Chat.LabelError = lipgloss.NewStyle().Foreground(theme.Red)
	s.Chat.LabelSystem = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Chat.LabelPlan = lipgloss.NewStyle().Foreground(theme.Blue)
	s.Chat.LabelInfo = lipgloss.NewStyle().Foreground(theme.FgMuted)

	inputBase := lipgloss.NewStyle()
	s.Input.Textarea = textarea.Styles{
		Focused: textarea.StyleState{
			Base:        inputBase,
			Text:        inputBase.Foreground(theme.Fg),
			CursorLine:  inputBase,
			Placeholder: inputBase.Foreground(theme.FgMuted),
			Prompt:      inputBase.Foreground(theme.Primary),
		},
		Blurred: textarea.StyleState{
			Base:        inputBase,
			Text:        inputBase.Foreground(theme.FgSubtle),
			CursorLine:  inputBase,
			Placeholder: inputBase.Foreground(theme.FgMuted),
			Prompt:      inputBase.Foreground(theme.FgMuted),
		},
		Cursor: textarea.CursorStyle{
			Color: theme.Fg,
			Shape: tea.CursorBlock,
			Blink: true,
		},
	}
	s.Input.Box = lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	s.Input.BoxIdle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(theme.FgMuted)
	s.Input.BoxBusy = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(theme.Primary)

	s.Modal.Title = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Modal.Help = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Modal.Cursor = lipgloss.NewStyle().Foreground(theme.Green)
	s.Modal.Tool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Modal.ToolArg = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Picker.Title = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Picker.Cursor = lipgloss.NewStyle().Foreground(theme.Green)
	s.Picker.Item = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Picker.Help = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Picker.Filter = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Picker.Command = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Picker.Info = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Picker.Preview = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Headline = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Queued = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Status.Separator = lipgloss.NewStyle().Foreground(theme.FgMuted).Faint(true)
	s.Status.Model = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Status.Context = lipgloss.NewStyle().Foreground(theme.Blue)
	s.Status.Tokens = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Status.Calls = lipgloss.NewStyle().Foreground(theme.Green)
	s.Status.Tool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Status.Scroll = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Task.Label = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Task.Pending = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Task.InProgress = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Task.Completed = lipgloss.NewStyle().Foreground(theme.Green)
	s.Agent.Label = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Agent.Running = lipgloss.NewStyle().Foreground(theme.Magenta)

	return s
}
