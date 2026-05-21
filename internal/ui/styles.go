package ui

import (
	"cece/internal/ui/theme"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Styles holds all TUI style definitions.
type Styles struct {
	Chat struct {
		UserMsg         lipgloss.Style
		Assistant       lipgloss.Style
		AssistantBg     lipgloss.Style // background fill for assistant message area
		AssistantPrefix lipgloss.Style
		Divider         lipgloss.Style
		RequestLabel    lipgloss.Style
		ResponseLabel   lipgloss.Style
		ToolCallName    lipgloss.Style
		ToolCallArgs    lipgloss.Style
		ToolCallRun     lipgloss.Style
		ToolCallOk      lipgloss.Style
		ToolCallErr     lipgloss.Style
		ToolCallSummary lipgloss.Style
		ToolCallOutput  lipgloss.Style
		ThinkingLabel   lipgloss.Style // "Thought" prefix label
		ThinkingContent lipgloss.Style // expanded thinking content area
		ThinkingBg      lipgloss.Style // background fill for thinking block lines
		Diff            DiffStyles
	}
	Input struct {
		Prompt     lipgloss.Style
		PromptCont lipgloss.Style
		Textarea   textarea.Styles
		Separator  lipgloss.Style
		Box        lipgloss.Style // outer border style for the floating input box
		BoxFocused lipgloss.Style // focused state border
		BoxBlurred lipgloss.Style // blurred state border
	}
	StatusBar struct {
		Pill          lipgloss.Style // status pill (idle)
		PillActive    lipgloss.Style // status pill (busy)
		Model         lipgloss.Style // model name text
		Project       lipgloss.Style // git branch + workdir
		Separator     lipgloss.Style // section separator │
		ContextInfo   lipgloss.Style // context/token usage
		ContextGood   lipgloss.Style // healthy remaining context
		ContextWarn   lipgloss.Style // low remaining context
		ContextDanger lipgloss.Style // critical remaining context
		ContextEmpty  lipgloss.Style // consumed context cells
		KeyHint       lipgloss.Style // keyboard shortcut hints
	}
	Detail lipgloss.Style
	Status lipgloss.Style
}

// DefaultStyles returns the default style set built from the theme palette.
func DefaultStyles() Styles {
	return BuildStyles(theme.DefaultPalette())
}

// BuildStyles constructs a Styles from a theme.Palette.
// All color assignments are semantic — swap the palette and the entire UI adapts.
func BuildStyles(p theme.Palette) Styles {
	var (
		base  = lipgloss.NewStyle().Foreground(p.FgBase)
		muted = lipgloss.NewStyle().Foreground(p.FgMuted)
		faint = lipgloss.NewStyle().Foreground(p.FgFaint)
		s     Styles
	)

	// Chat — user message with left padding.
	s.Chat.UserMsg = base.Background(p.BgSubtle).Padding(1, 2)
	s.Chat.Assistant = base
	s.Chat.AssistantBg = lipgloss.NewStyle()
	s.Chat.AssistantPrefix = base.Foreground(p.Success)
	s.Chat.Divider = faint
	s.Chat.RequestLabel = base.Foreground(p.SuccessMuted)
	s.Chat.ResponseLabel = base.Foreground(p.WarningMuted)
	s.Chat.ToolCallName = base.Foreground(p.Info).Bold(true)
	s.Chat.ToolCallArgs = muted
	s.Chat.ToolCallRun = base.Foreground(p.Busy)
	s.Chat.ToolCallOk = base.Foreground(p.Success)
	s.Chat.ToolCallErr = base.Foreground(p.Destructive)
	s.Chat.ToolCallSummary = muted
	s.Chat.ToolCallOutput = muted

	// Thinking — collapsible block, no background; content uses faint color.
	s.Chat.ThinkingLabel = faint.Foreground(p.SuccessMuted).Italic(true)
	s.Chat.ThinkingContent = lipgloss.NewStyle().Foreground(p.FgFaint).Italic(true)
	s.Chat.ThinkingBg = lipgloss.NewStyle()

	// Diff
	s.Chat.Diff.DeleteLine = base.
		Foreground(p.Destructive).
		Background(lipgloss.Color("#3b1111"))
	s.Chat.Diff.InsertLine = base.
		Foreground(p.SuccessMuted).
		Background(lipgloss.Color("#113b1b"))
	s.Chat.Diff.ContextLine = faint
	s.Chat.Diff.Summary = base.Foreground(p.Secondary).Bold(true)

	// Input — floating rounded box style.
	inputBase := lipgloss.NewStyle()
	s.Input.Prompt = lipgloss.NewStyle().Foreground(p.Primary)
	s.Input.PromptCont = lipgloss.NewStyle().Foreground(p.FgFaint)
	s.Input.Textarea = textarea.Styles{
		Focused: textarea.StyleState{
			Base:        inputBase,
			Text:        inputBase.Foreground(p.FgBase),
			CursorLine:  inputBase,
			Placeholder: inputBase.Foreground(p.FgFaint),
			Prompt:      inputBase.Foreground(p.Primary),
		},
		Blurred: textarea.StyleState{
			Base:        inputBase,
			Text:        inputBase.Foreground(p.FgMuted),
			CursorLine:  inputBase,
			Placeholder: inputBase.Foreground(p.FgFaint),
			Prompt:      inputBase.Foreground(p.FgMuted),
		},
		Cursor: textarea.CursorStyle{
			Color: p.FgBase,
			Shape: tea.CursorBlock,
			Blink: true,
		},
	}
	s.Input.Separator = lipgloss.NewStyle().Foreground(p.Separator)

	// Floating thick box border (thick lines with sharp corners —
	// Unicode has no thick rounded corners, so we use thick square corners
	// for a consistent bold look).
	boxBase := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder()).
		Padding(0, 1)
	s.Input.BoxFocused = boxBase.
		BorderForeground(p.Primary)
	s.Input.BoxBlurred = boxBase.
		BorderForeground(p.Separator)

	// StatusBar — flat single-line indicator, no pill background.
	s.StatusBar.Pill = lipgloss.NewStyle().
		Foreground(p.FgMuted)
	s.StatusBar.PillActive = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true)
	s.StatusBar.Model = base.Foreground(p.Info)
	s.StatusBar.Project = muted
	s.StatusBar.Separator = faint
	s.StatusBar.ContextInfo = muted
	s.StatusBar.ContextGood = base.Foreground(p.Success)
	s.StatusBar.ContextWarn = base.Foreground(p.Warning)
	s.StatusBar.ContextDanger = base.Foreground(p.Destructive)
	s.StatusBar.ContextEmpty = faint
	s.StatusBar.KeyHint = faint.Italic(true)

	// Detail & Status
	s.Detail = muted.Italic(true).Faint(true)
	s.Status = muted.Faint(true)

	// Markdown renderer uses no background to blend into the terminal.
	SetMarkdownBackground("")

	return s
}
