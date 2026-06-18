package ui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zhanglvtao/cece/internal/ui/theme"
)

// Styles holds all TUI style definitions built from ANSI terminal colors.
type Styles struct {
	Chat struct {
		LabelUser      lipgloss.Style
		LabelAssistant lipgloss.Style
		AssistantBody  lipgloss.Style
		LabelThinking  lipgloss.Style
		LabelTool      lipgloss.Style
		LabelError     lipgloss.Style
		LabelSystem    lipgloss.Style
		LabelPlan      lipgloss.Style
		LabelInfo      lipgloss.Style
		LabelView      lipgloss.Style
	}
	Input struct {
		Textarea textarea.Styles
		Box      lipgloss.Style
		BoxBusy  lipgloss.Style
		BoxIdle  lipgloss.Style
		BoxShell lipgloss.Style
	}
	Modal struct {
		Title   lipgloss.Style
		Help    lipgloss.Style
		Cursor  lipgloss.Style
		Tool    lipgloss.Style
		ToolArg lipgloss.Style
	}
	Picker struct {
		Title        lipgloss.Style
		Cursor       lipgloss.Style
		Item         lipgloss.Style // non-selected item text
		SelectedItem lipgloss.Style // selected item text
		Help         lipgloss.Style
		Filter       lipgloss.Style
		Command      lipgloss.Style
		Info         lipgloss.Style // secondary info: model, time
		Preview      lipgloss.Style // preview text below title
	}
	Headline lipgloss.Style
	Queued   lipgloss.Style
	TitleBar lipgloss.Style
	Status   struct {
		Separator lipgloss.Style
		Model     lipgloss.Style
		Context   lipgloss.Style
		Tokens    lipgloss.Style
		Calls     lipgloss.Style
		Ok        lipgloss.Style // ✓ green
		Fail      lipgloss.Style // ✗ red
		Tool      lipgloss.Style // default / unclassified tools + MCP
		ToolFile  lipgloss.Style // file ops: Read, Write, Edit, Glob, Grep, Bash
		ToolWeb   lipgloss.Style // web ops: WebFetch, WebSearch
		ToolAsk   lipgloss.Style // user interaction: AskUserQuestion
		ToolCtx   lipgloss.Style // context compression: Compact, Trim, Prune
		ToolAgent lipgloss.Style // Agent sub-agent
		ToolPlan  lipgloss.Style // EnterPlanMode, ExitPlanMode
		Scroll    lipgloss.Style
	}
	Task struct {
		Label      lipgloss.Style
		Pending    lipgloss.Style
		InProgress lipgloss.Style
		Completed  lipgloss.Style
	}
	Agent struct {
		Label     lipgloss.Style
		Running   lipgloss.Style
		Done      lipgloss.Style
		Completed lipgloss.Style
	}
}

// DefaultStyles returns the style set built from ANSI terminal colors.
func DefaultStyles() Styles {
	var s Styles

	s.Chat.LabelUser = lipgloss.NewStyle().Foreground(theme.Fg)
	s.Chat.LabelAssistant = lipgloss.NewStyle().Foreground(theme.Green)
	s.Chat.AssistantBody = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Chat.LabelThinking = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Chat.LabelTool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Chat.LabelError = lipgloss.NewStyle().Foreground(theme.Red)
	s.Chat.LabelSystem = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Chat.LabelPlan = lipgloss.NewStyle().Foreground(theme.Blue)
	s.Chat.LabelInfo = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Chat.LabelView = lipgloss.NewStyle().Foreground(theme.Primary)

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
	s.Input.BoxShell = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(theme.Yellow)

	s.Modal.Title = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Modal.Help = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Modal.Cursor = lipgloss.NewStyle().Foreground(theme.Green)
	s.Modal.Tool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Modal.ToolArg = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Picker.Title = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Picker.Cursor = lipgloss.NewStyle().Foreground(theme.Green)
	s.Picker.Item = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Picker.SelectedItem = lipgloss.NewStyle().Foreground(theme.Fg).Bold(true)
	s.Picker.Help = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Picker.Filter = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Picker.Command = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Picker.Info = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Picker.Preview = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Headline = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Queued = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.TitleBar = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Status.Separator = lipgloss.NewStyle().Foreground(theme.FgMuted).Faint(true)
	s.Status.Model = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Status.Context = lipgloss.NewStyle().Foreground(theme.Blue)
	s.Status.Tokens = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Status.Calls = lipgloss.NewStyle().Foreground(theme.FgSubtle)
	s.Status.Ok = lipgloss.NewStyle().Foreground(theme.Green)
	s.Status.Fail = lipgloss.NewStyle().Foreground(theme.Red)
	s.Status.Tool = lipgloss.NewStyle().Foreground(theme.Yellow)
	s.Status.ToolFile = lipgloss.NewStyle().Foreground(theme.Green)    // file ops
	s.Status.ToolWeb = lipgloss.NewStyle().Foreground(theme.Magenta)   // web ops
	s.Status.ToolAsk = lipgloss.NewStyle().Foreground(theme.Blue)      // user interaction
	s.Status.ToolCtx = lipgloss.NewStyle().Foreground(theme.Blue)      // context compression
	s.Status.ToolAgent = lipgloss.NewStyle().Foreground(theme.Magenta) // Agent sub-agent
	s.Status.ToolPlan = lipgloss.NewStyle().Foreground(theme.Primary)  // plan/unplan
	s.Status.Scroll = lipgloss.NewStyle().Foreground(theme.FgMuted)

	s.Task.Label = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Task.Pending = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Task.InProgress = lipgloss.NewStyle().Foreground(theme.Primary)
	s.Task.Completed = lipgloss.NewStyle().Foreground(theme.Green)
	s.Agent.Label = lipgloss.NewStyle().Foreground(theme.Magenta)
	s.Agent.Running = lipgloss.NewStyle().Foreground(theme.Magenta)
	s.Agent.Done = lipgloss.NewStyle().Foreground(theme.FgMuted)
	s.Agent.Completed = lipgloss.NewStyle().Foreground(theme.Green)

	return s
}
