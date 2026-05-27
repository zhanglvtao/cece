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
		Logo            lipgloss.Style
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
		Box             lipgloss.Style // rounded border for chat area (idle/static)
		Diff            DiffStyles
	}
	Input struct {
		Prompt            lipgloss.Style
		PromptCont        lipgloss.Style
		Textarea          textarea.Styles
		Separator         lipgloss.Style
		Box               lipgloss.Style // outer border style for the floating input box
		BoxFocused        lipgloss.Style // focused state border
		BoxBlurred        lipgloss.Style // blurred state border
		BoxBusy           lipgloss.Style // busy state border (thick)
		BoxIdle           lipgloss.Style // idle state border (thin)
		SlashCommand      lipgloss.Style
		SlashCommandMatch lipgloss.Style
		SlashPopup        lipgloss.Style
		SlashPopupTitle   lipgloss.Style
		SlashPopupDesc    lipgloss.Style
		SlashPopupSelected lipgloss.Style
	}
	StatusBar struct {
		ModelPill lipgloss.Style // model name in pill style
		ToolCount lipgloss.Style // tool call count (e.g. Grep:3)
		Scroll    lipgloss.Style // scroll position indicator
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

	// Chat — user message, no padding.
	s.Chat.UserMsg = base
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

	// Chat box — rounded border for idle/static state.
	s.Chat.Box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Separator)
	s.Chat.Logo = base.Foreground(p.Primary).Bold(true)

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

	s.Input.SlashCommand = base.Foreground(p.Primary)
	s.Input.SlashCommandMatch = base.Foreground(p.Keyword).Underline(true)
	s.Input.SlashPopup = base.Foreground(p.FgMuted)
	s.Input.SlashPopupSelected = base.Bold(true)

	// StatusBar — model pill, tool counts, scroll indicator.
	s.StatusBar.ModelPill = lipgloss.NewStyle().
		Foreground(p.Primary).
		Background(lipgloss.Color("#2a2a3a")).
		Bold(true).
		Padding(0, 1)
	s.StatusBar.ToolCount = base.Foreground(p.InfoMuted)
	s.StatusBar.Scroll = base.Foreground(p.FgFaint)

	// Detail & Status
	s.Detail = muted.Italic(true).Faint(true)
	s.Status = muted.Faint(true)

	// Markdown renderer uses no background to blend into the terminal.
	SetMarkdownBackground("")

	return s
}
