package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/ui/picker"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalConfirmTools
	modalApprovePlan
	modalQuestion
	modalModelPicker
	modalSessionPicker
)

type modalState struct {
	kind      modalKind
	calls     []protocol.ToolUseBlock
	questions []protocol.Question
	qIndex    int
	cursors   []int
	selected  map[int][]int
	custom    map[int]string
	textMode  bool
	textInput string
	picker    *picker.Picker
	planFile  string
}

func (m modalState) active() bool { return m.kind != modalNone }

func (m *Model) modalHeight() int {
	if !m.modal.active() {
		return 0
	}
	if m.modal.kind == modalModelPicker || m.modal.kind == modalSessionPicker {
		if m.modal.picker != nil {
			return m.modal.picker.Height() + 1 // +1 for separator line
		}
		return 0
	}
	lines := strings.Count(m.modalView(), "\n") + 1
	return clamp(lines, 3, modalMaxHeight)
}

func (m *Model) modalView() string {
	if !m.modal.active() {
		return ""
	}
	var body string
	switch m.modal.kind {
	case modalConfirmTools:
		body = m.confirmToolsView()
	case modalApprovePlan:
		body = fmt.Sprintf("Approve plan %s?\n[y/enter] approve  [n/esc] reject", m.modal.planFile)
	case modalQuestion:
		body = m.questionView()
	case modalModelPicker, modalSessionPicker:
		if m.modal.picker != nil {
			body = m.modal.picker.View()
		}
	}
	return body
}

func (m *Model) handleModalKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.modal.kind {
	case modalConfirmTools:
		return m.handleConfirmToolsKey(msg)
	case modalApprovePlan:
		return m.handleApprovePlanKey(msg)
	case modalQuestion:
		return m.handleQuestionKey(msg)
	case modalModelPicker, modalSessionPicker:
		return m.handlePickerKey(msg)
	}
	return nil
}

func (m *Model) openToolConfirm(calls []protocol.ToolUseBlock) {
	m.modal = modalState{kind: modalConfirmTools, calls: calls}
}

func (m *Model) confirmToolsView() string {
	var b strings.Builder
	b.WriteString("Allow tool calls?\n")
	for _, call := range m.modal.calls {
		args := formatJSONPreview(call.Input)
		args = strings.ReplaceAll(args, "\n", " ")
		b.WriteString("- " + call.Name)
		if args != "" {
			b.WriteString("  " + ansi.Truncate(args, max(20, m.width-20), "..."))
		}
		b.WriteByte('\n')
	}
	b.WriteString("[y/enter] allow  [n/esc] deny")
	return b.String()
}

func (m *Model) handleConfirmToolsKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y", "enter":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ConfirmAction{})
		}
	case "n", "esc":
		m.modal = modalState{}
		m.cancelTurn("Tool calls rejected")
	}
	return nil
}

func (m *Model) openPlanConfirm(planFile string) {
	m.modal = modalState{kind: modalApprovePlan, planFile: planFile}
}

func (m *Model) handleApprovePlanKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y", "enter":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ApprovePlanAction{})
		}
	case "n", "esc":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.RejectPlanAction{})
		}
		m.busy = false
		m.mode = protocol.PermissionModePlan
		m.status = "Plan rejected"
	}
	return nil
}

func (m *Model) openQuestion(questions []protocol.Question) {
	m.modal = modalState{
		kind:      modalQuestion,
		questions: questions,
		cursors:   make([]int, len(questions)),
		selected:  make(map[int][]int),
		custom:    make(map[int]string),
	}
}

func (m *Model) questionView() string {
	if len(m.modal.questions) == 0 {
		return "Question\n[esc] cancel"
	}
	q := m.modal.questions[m.modal.qIndex]
	var b strings.Builder
	fmt.Fprintf(&b, "Question %d/%d\n%s\n", m.modal.qIndex+1, len(m.modal.questions), q.Question)
	if q.Preview != "" {
		b.WriteString(summarizeText(q.Preview, 2000, 8) + "\n")
	}
	optionCount := len(q.Options) + 1
	for i := 0; i < optionCount; i++ {
		cursor := " "
		mark := "[ ]"
		if i == len(q.Options) {
			// "Type something else..." — inline text input
			if m.modal.textMode {
				cursor = ">"
				displayLabel := m.modal.textInput
				if displayLabel == "" {
					displayLabel = "Type something else..."
				}
				fmt.Fprintf(&b, "%s %s %s\n", cursor, mark, displayLabel)
			} else {
				if i == m.modal.cursors[m.modal.qIndex] {
					cursor = ">"
				}
				label := "Type something else..."
				if custom, ok := m.modal.custom[m.modal.qIndex]; ok && custom != "" {
					label = custom
					mark = "[x]"
				}
				fmt.Fprintf(&b, "%s %s %s\n", cursor, mark, label)
			}
		} else {
			if i == m.modal.cursors[m.modal.qIndex] && !m.modal.textMode {
				cursor = ">"
			}
			if m.questionSelected(m.modal.qIndex, i) {
				mark = "[x]"
			}
			fmt.Fprintf(&b, "%s %s %s\n", cursor, mark, q.Options[i].Label)
		}
	}
	b.WriteString("[up/down] move  [space] toggle  [enter] next  [esc] cancel")
	return b.String()
}

func (m *Model) handleQuestionKey(msg tea.KeyPressMsg) tea.Cmd {
	if len(m.modal.questions) == 0 {
		if msg.String() == "esc" {
			m.modal = modalState{}
			m.cancelTurn("Question cancelled")
		}
		return nil
	}
	q := m.modal.questions[m.modal.qIndex]
	if m.modal.textMode {
		switch msg.String() {
		case "enter":
			m.modal.custom[m.modal.qIndex] = m.modal.textInput
			m.modal.textInput = ""
			m.modal.textMode = false
			return m.advanceQuestion()
		case "esc":
			m.modal.textInput = ""
			m.modal.textMode = false
		case "backspace":
			if m.modal.textInput != "" {
				_, size := utf8.DecodeLastRuneInString(m.modal.textInput)
				m.modal.textInput = m.modal.textInput[:len(m.modal.textInput)-size]
			}
		default:
			if text := msg.Key().Text; text != "" {
				m.modal.textInput += text
			}
		}
		return nil
	}
	optionCount := len(q.Options) + 1
	switch msg.String() {
	case "up", "k":
		if m.modal.cursors[m.modal.qIndex] > 0 {
			m.modal.cursors[m.modal.qIndex]--
		}
	case "down", "j":
		if m.modal.cursors[m.modal.qIndex] < optionCount-1 {
			m.modal.cursors[m.modal.qIndex]++
		}
	case "left", "h":
		if m.modal.qIndex > 0 {
			m.saveQuestionText()
			m.modal.qIndex--
			m.restoreQuestionText()
		}
	case "right", "l":
		if m.modal.qIndex < len(m.modal.questions)-1 {
			m.saveQuestionText()
			m.modal.qIndex++
			m.restoreQuestionText()
		}
	case "space":
		cursor := m.modal.cursors[m.modal.qIndex]
		if cursor < len(q.Options) {
			m.toggleQuestionSelection(m.modal.qIndex, cursor)
		}
	case "enter":
		cursor := m.modal.cursors[m.modal.qIndex]
		if cursor == len(q.Options) {
			m.modal.textMode = true
			return nil
		}
		if q.MultiSelect {
			if len(m.modal.selected[m.modal.qIndex]) == 0 {
				return nil
			}
		} else {
			m.modal.selected[m.modal.qIndex] = []int{cursor}
		}
		return m.advanceQuestion()
	case "esc", "ctrl+c":
		m.modal = modalState{}
		m.cancelTurn("Question cancelled")
	}
	return nil
}

// saveQuestionText persists the current textInput into custom map if in textMode.
func (m *Model) saveQuestionText() {
	if m.modal.textMode && m.modal.textInput != "" {
		m.modal.custom[m.modal.qIndex] = m.modal.textInput
	}
}

// restoreQuestionText restores textInput and textMode from custom map for the current question.
func (m *Model) restoreQuestionText() {
	if custom, ok := m.modal.custom[m.modal.qIndex]; ok && custom != "" {
		m.modal.textMode = true
		m.modal.textInput = custom
	} else {
		m.modal.textMode = false
		m.modal.textInput = ""
	}
}

func (m *Model) advanceQuestion() tea.Cmd {
	if m.modal.qIndex < len(m.modal.questions)-1 {
		m.modal.qIndex++
		return nil
	}
	answers := m.questionAnswers()
	m.modal = modalState{}
	if actor, ok := m.sender.(Actor); ok {
		actor.Do(protocol.AnswerQuestionAction{Answers: answers})
	}
	m.status = "Answered"
	return nil
}

func (m *Model) questionAnswers() []protocol.QuestionAnswer {
	answers := make([]protocol.QuestionAnswer, len(m.modal.questions))
	for i, q := range m.modal.questions {
		ans := protocol.QuestionAnswer{Question: q.Question}
		if custom := m.modal.custom[i]; custom != "" {
			ans.Custom = custom
		} else {
			for _, idx := range m.modal.selected[i] {
				if idx >= 0 && idx < len(q.Options) {
					ans.Selected = append(ans.Selected, q.Options[idx].Label)
				}
			}
		}
		answers[i] = ans
	}
	return answers
}

func (m *Model) questionSelected(qIdx, optIdx int) bool {
	for _, idx := range m.modal.selected[qIdx] {
		if idx == optIdx {
			return true
		}
	}
	return false
}

func (m *Model) toggleQuestionSelection(qIdx, optIdx int) {
	selected := m.modal.selected[qIdx]
	for i, idx := range selected {
		if idx == optIdx {
			m.modal.selected[qIdx] = append(selected[:i], selected[i+1:]...)
			return
		}
	}
	m.modal.selected[qIdx] = append(selected, optIdx)
}

func (m *Model) openModelPicker(models []protocol.ModelInfo) {
	items := make([]any, len(models))
	for i, model := range models {
		items[i] = model
	}
	p := picker.New("Switch model", items, modalMaxHeight, func(item any, selected bool) string {
		mi := item.(protocol.ModelInfo)
		name := mi.DisplayName
		if name == "" {
			name = mi.ID
		}
		provider := mi.Provider
		if provider != "" {
			provider += "/"
		}
		return picker.FormatItem(provider+name, selected)
	})
	p.SetFilterFn(func(item any, q string) bool {
		mi := item.(protocol.ModelInfo)
		return strings.Contains(strings.ToLower(mi.ID+" "+mi.DisplayName+" "+mi.Provider), strings.ToLower(q))
	})
	p.SetHelpText("[up/down] move  [enter] select  [type] filter  [esc] close")
	p.SetOnSelect(func(item any) tea.Cmd {
		mi := item.(protocol.ModelInfo)
		m.modelName = mi.ID
		m.statusBar.UpdateModel(mi.ID)
		if mi.MaxContextWindow > 0 {
			m.contextWindow = mi.MaxContextWindow
			m.statusBar.UpdateContext(m.transcript.contextUsed, m.contextWindow)
		}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.SwitchModelAction{
				Model:            mi.ID,
				MaxContextWindow: mi.MaxContextWindow,
				APIKey:           mi.APIKey,
				BaseURL:          mi.BaseURL,
				AuthMode:         mi.AuthMode,
				AuthHelper:       mi.AuthHelper,
				Protocol:         mi.Protocol,
				ConfigName:       mi.ConfigName,
			})
		}
		m.status = "Model switched"
		return nil
	})
	// Pre-select current model
	for i, model := range models {
		if model.ID == m.modelName {
			for j := 0; j < i; j++ {
				p.Down()
			}
			break
		}
	}
	m.modal = modalState{kind: modalModelPicker, picker: p}
}

func (m *Model) handlePickerKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.modal.picker == nil {
		return nil
	}
	result, cmd := m.modal.picker.HandleKey(msg)
	if result == picker.ResultClose {
		m.modal = modalState{}
	}
	return cmd
}

func (m *Model) openSessionsDialog() {
	if m.sessions == nil {
		m.status = "Sessions not available"
		return
	}
	sessions, err := m.sessions.List(context.Background())
	if err != nil {
		m.status = "Failed to list sessions: " + err.Error()
		return
	}
	items := make([]any, len(sessions))
	for i, s := range sessions {
		items[i] = s
	}
	p := picker.New("Resume session", items, modalMaxHeight, func(item any, selected bool) string {
		s := item.(session.Session)
		title := s.Title
		if title == "" {
			title = s.ID
		}
		return picker.FormatItem(title+"  "+s.UpdatedAt.Format("2006-01-02 15:04"), selected)
	})
	p.SetHelpText("[up/down] move  [enter] load  [esc] close")
	p.SetOnSelect(func(item any) tea.Cmd {
		s := item.(session.Session)
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.LoadSessionAction{SessionID: s.ID})
		}
		m.status = "Loading session"
		return nil
	})
	m.modal = modalState{kind: modalSessionPicker, picker: p}
	m.status = "Resume session"
}
