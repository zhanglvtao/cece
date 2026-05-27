package dialog

import (
	"fmt"
	"strings"

	"cece/internal/protocol"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

const QuestionDialogID = "question-dialog"

const typeSomethingLabel = "Type something else..."

// ActionAnswerQuestion signals that the user has answered all questions.
type ActionAnswerQuestion struct {
	Answers []protocol.QuestionAnswer
}

// QuestionDialog is a dialog that asks the user one or more questions.
type QuestionDialog struct {
	styles      DialogStyles
	help        help.Model
	questions   []protocol.Question
	current     int            // current question index
	cursors     []int          // cursor position per question
	selected    map[int][]int  // question index -> selected option indices
	customTexts map[int]string // question index -> custom free-text answer
	textInput   string         // current text input buffer
	inTextMode  bool           // true when user is typing a custom answer
	finished    bool
}

var _ Dialog = (*QuestionDialog)(nil)

// NewQuestionDialog creates a new question dialog.
func NewQuestionDialog(styles DialogStyles, questions []protocol.Question) *QuestionDialog {
	d := &QuestionDialog{
		styles:      styles,
		questions:   questions,
		cursors:     make([]int, len(questions)),
		selected:    make(map[int][]int),
		customTexts: make(map[int]string),
	}
	d.help = help.New()
	return d
}

// ID implements Dialog.
func (d *QuestionDialog) ID() string { return QuestionDialogID }

// DesiredHeight implements Dialog.
func (d *QuestionDialog) DesiredHeight() int {
	h := 14
	if d.current < len(d.questions) && d.questions[d.current].Preview != "" {
		h += strings.Count(d.questions[d.current].Preview, "\n") + 1
	}
	return h
}

// HandleMsg implements Dialog.
func (d *QuestionDialog) HandleMsg(msg tea.Msg) Action {
	if d.finished {
		return nil
	}

	q := d.questions[d.current]
	optionCount := len(q.Options) + 1 // +1 for "Type something else..."

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if d.inTextMode {
			return d.handleTextMode(msg)
		}
		switch msg.String() {
		case "up", "k":
			if d.cursors[d.current] > 0 {
				d.cursors[d.current]--
			}
			return ActionConsumed{}
		case "down", "j":
			if d.cursors[d.current] < optionCount-1 {
				d.cursors[d.current]++
			}
			return ActionConsumed{}
		case "left", "h":
			if d.current > 0 {
				d.current--
				d.inTextMode = false
				d.textInput = ""
			}
			return ActionConsumed{}
		case "right", "l":
			if d.current < len(d.questions)-1 {
				d.current++
				d.inTextMode = false
				d.textInput = ""
			}
			return ActionConsumed{}
		case "space":
			cursor := d.cursors[d.current]
			if cursor < len(q.Options) {
				d.toggleSelect(d.current, cursor)
			}
			return ActionConsumed{}
		case "enter":
			cursor := d.cursors[d.current]
			if cursor == len(q.Options) {
				// "Type something else..." selected
				d.inTextMode = true
				d.textInput = ""
				return ActionConsumed{}
			}
			if q.MultiSelect {
				// multiSelect: enter confirms current selections
				return d.confirmCurrentQuestion()
			}
			// single-select: enter selects this option and moves on
			d.selected[d.current] = []int{cursor}
			return d.advance()
		case "esc":
			d.finished = true
			return ActionCancelQuestion{}
		case "ctrl+c":
			d.finished = true
			return ActionCancelQuestion{}
		}
	}
	return nil
}

func (d *QuestionDialog) handleTextMode(msg tea.KeyPressMsg) Action {
	switch msg.String() {
	case "enter":
		d.inTextMode = false
		return d.advance()
	case "esc":
		d.inTextMode = false
		d.textInput = ""
		return ActionConsumed{}
	case "ctrl+c":
		d.finished = true
		return ActionCancelQuestion{}
	case "backspace":
		if len(d.textInput) > 0 {
			d.textInput = d.textInput[:len(d.textInput)-1]
		}
		return ActionConsumed{}
	default:
		// Accept printable characters via Text field
		k := msg.Key()
		if k.Text != "" && k.Text != " " && k.Mod == 0 {
			d.textInput += k.Text
		}
		return ActionConsumed{}
	}
}

func (d *QuestionDialog) toggleSelect(qIdx, optIdx int) {
	sel := d.selected[qIdx]
	for i, s := range sel {
		if s == optIdx {
			d.selected[qIdx] = append(sel[:i], sel[i+1:]...)
			return
		}
	}
	d.selected[qIdx] = append(sel, optIdx)
}

func (d *QuestionDialog) confirmCurrentQuestion() Action {
	q := d.questions[d.current]
	if q.MultiSelect {
		if len(d.selected[d.current]) == 0 {
			return ActionConsumed{}
		}
		return d.advance()
	}
	cursor := d.cursors[d.current]
	if cursor < len(q.Options) {
		d.selected[d.current] = []int{cursor}
	}
	return d.advance()
}

func (d *QuestionDialog) advance() Action {
	if d.textInput != "" {
		d.customTexts[d.current] = d.textInput
	}

	if d.current < len(d.questions)-1 {
		d.current++
		d.inTextMode = false
		d.textInput = ""
		return ActionConsumed{}
	}
	d.finished = true
	return ActionAnswerQuestion{Answers: d.buildAnswers()}
}

func (d *QuestionDialog) buildAnswers() []protocol.QuestionAnswer {
	answers := make([]protocol.QuestionAnswer, len(d.questions))
	for i, q := range d.questions {
		ans := protocol.QuestionAnswer{Question: q.Question}
		if custom, ok := d.customTexts[i]; ok && custom != "" {
			ans.Custom = custom
		} else {
			for _, idx := range d.selected[i] {
				if idx < len(q.Options) {
					ans.Selected = append(ans.Selected, q.Options[idx].Label)
				}
			}
		}
		answers[i] = ans
	}
	return answers
}

// Draw implements Dialog.
func (d *QuestionDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.styles
	width := max(0, area.Dx()-t.View.GetHorizontalBorderSize())

	rc := NewRenderContext(t.Title, t.View, width)
	rc.Gap = 1

	// Title: "Question (1/3)"
	if len(d.questions) > 1 {
		rc.Title = fmt.Sprintf("Question (%d/%d)", d.current+1, len(d.questions))
	} else {
		rc.Title = "Question"
	}

	q := d.questions[d.current]

	// Question text
	lines := []string{"  " + t.InfoFocused.Render(q.Question)}

	// Preview panel (if provided)
	if q.Preview != "" {
		previewPanel := t.ContentPanel.Width(width - t.View.GetHorizontalFrameSize()).Render("  " + t.InfoBlurred.Render(q.Preview))
		rc.AddPart(previewPanel)
	}

	optionCount := len(q.Options) + 1 // +1 for "Type something else..."
	cursor := d.cursors[d.current]

	for i := 0; i < optionCount; i++ {
		var label, desc string
		if i < len(q.Options) {
			label = q.Options[i].Label
			desc = q.Options[i].Description
		} else {
			label = typeSomethingLabel
		}

		prefix := "  "
		if d.isSelected(d.current, i) {
			prefix = "  ◉ "
		} else {
			prefix = "  ○ "
		}

		line := prefix + label
		if desc != "" && i == cursor && !d.inTextMode {
			line += "  " + t.InfoBlurred.Render(desc)
		}

		if i == cursor && !d.inTextMode {
			line = "▸" + line[1:]
			line = t.SelectedItem.Render(line)
		} else {
			line = " " + line[1:]
			line = t.NormalItem.Render(line)
		}
		lines = append(lines, line)
	}

	// Text input mode
	if d.inTextMode {
		lines = append(lines, "")
		inputStyle := t.InputPrompt.Width(width - t.View.GetHorizontalFrameSize() - 4)
		inputText := d.textInput
		if inputText == "" {
			inputText = "..."
		}
		lines = append(lines, "  "+inputStyle.Render(inputText))
	}

	panelContent := strings.Join(lines, "\n")
	rc.AddPart(t.ContentPanel.Width(width - t.View.GetHorizontalFrameSize()).Render(panelContent))

	// Help
	if d.inTextMode {
		rc.Help = fmt.Sprintf("%s  %s  %s",
			t.AllowBtn.Render("[enter] confirm"),
			t.DenyBtn.Render("[esc] cancel"),
			t.DenyBtn.Render("[ctrl+c] cancel"),
		)
	} else if len(d.questions) > 1 {
		if q.MultiSelect {
			rc.Help = fmt.Sprintf("%s  %s  %s  %s  %s",
				"[↑↓] navigate",
				t.AllowBtn.Render("[space] toggle"),
				t.AllowBtn.Render("[enter] confirm"),
				"[←→] questions",
				t.DenyBtn.Render("[esc] cancel"),
			)
		} else {
			rc.Help = fmt.Sprintf("%s  %s  %s  %s  %s",
				"[↑↓] navigate",
				t.AllowBtn.Render("[space] toggle"),
				t.AllowBtn.Render("[enter] select"),
				"[←→] questions",
				t.DenyBtn.Render("[esc] cancel"),
			)
		}
	} else {
		if q.MultiSelect {
			rc.Help = fmt.Sprintf("%s  %s  %s  %s",
				"[↑↓] navigate",
				t.AllowBtn.Render("[space] toggle"),
				t.AllowBtn.Render("[enter] confirm"),
				t.DenyBtn.Render("[esc] cancel"),
			)
		} else {
			rc.Help = fmt.Sprintf("%s  %s  %s  %s",
				"[↑↓] navigate",
				t.AllowBtn.Render("[space] toggle"),
				t.AllowBtn.Render("[enter] select"),
				t.DenyBtn.Render("[esc] cancel"),
			)
		}
	}

	view := rc.Render()
	DrawInline(scr, area, view, nil)

	if d.inTextMode {
		cur := &tea.Cursor{}
		inputLineIdx := len(lines) - 1
		cur.X = area.Min.X + 4 + len(d.textInput)
		cur.Y = area.Min.Y + inputLineIdx
		return InputCursor(t.Title, t.View, t.InputPrompt, cur)
	}
	return nil
}

func (d *QuestionDialog) isSelected(qIdx, optIdx int) bool {
	for _, s := range d.selected[qIdx] {
		if s == optIdx {
			return true
		}
	}
	return false
}

// ShortHelp implements help.KeyMap.
func (d *QuestionDialog) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", "navigate")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select/confirm")),
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
		key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "prev question")),
		key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→", "next question")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// FullHelp implements help.KeyMap.
func (d *QuestionDialog) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}

// ensure QuestionDialog styles are available
func questionSelectedStyle(base lipgloss.Style) lipgloss.Style {
	return base.Bold(true)
}
