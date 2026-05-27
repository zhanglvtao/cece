package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"cece/internal/protocol"
	"cece/internal/session"
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
	models    []protocol.ModelInfo
	sessions  []session.Session
	selectedI int
	filter    string
	planFile  string
}

func (m modalState) active() bool { return m.kind != modalNone }

func (m *Model) modalHeight() int {
	if !m.modal.active() {
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
	case modalModelPicker:
		body = m.modelPickerView()
	case modalSessionPicker:
		body = m.sessionPickerView()
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
	case modalModelPicker:
		return m.handleModelPickerKey(msg)
	case modalSessionPicker:
		return m.handleSessionPickerKey(msg)
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
				fmt.Fprintf(&b, "%s %s %s\n", cursor, mark, "Type something else...")
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
			m.modal.qIndex--
		}
	case "right", "l":
		if m.modal.qIndex < len(m.modal.questions)-1 {
			m.modal.qIndex++
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
	m.modal = modalState{kind: modalModelPicker, models: models}
	for i, model := range models {
		if model.ID == m.modelName {
			m.modal.selectedI = i
			break
		}
	}
}

func (m *Model) modelPickerView() string {
	models := m.filteredModels()
	var b strings.Builder
	b.WriteString("Switch model")
	if m.modal.filter != "" {
		b.WriteString(" filter: " + m.modal.filter)
	}
	b.WriteByte('\n')
	if len(models) == 0 {
		b.WriteString("No models\n")
	} else {
		for i, model := range models {
			cursor := " "
			if i == m.modal.selectedI {
				cursor = ">"
			}
			name := model.DisplayName
			if name == "" {
				name = model.ID
			}
			provider := model.Provider
			if provider != "" {
				provider += "/"
			}
			fmt.Fprintf(&b, "%s %s%s\n", cursor, provider, name)
		}
	}
	b.WriteString("[up/down] move  [enter] select  [type] filter  [esc] close")
	return b.String()
}

func (m *Model) filteredModels() []protocol.ModelInfo {
	if m.modal.filter == "" {
		return m.modal.models
	}
	q := strings.ToLower(m.modal.filter)
	var out []protocol.ModelInfo
	for _, model := range m.modal.models {
		text := strings.ToLower(model.ID + " " + model.DisplayName + " " + model.Provider)
		if strings.Contains(text, q) {
			out = append(out, model)
		}
	}
	return out
}

func (m *Model) handleModelPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	models := m.filteredModels()
	switch msg.String() {
	case "esc":
		m.modal = modalState{}
	case "up", "ctrl+p":
		if len(models) > 0 {
			m.modal.selectedI = (m.modal.selectedI - 1 + len(models)) % len(models)
		}
	case "down", "ctrl+n":
		if len(models) > 0 {
			m.modal.selectedI = (m.modal.selectedI + 1) % len(models)
		}
	case "enter", "tab":
		if len(models) == 0 {
			return nil
		}
		selected := models[clamp(m.modal.selectedI, 0, len(models)-1)]
		m.modelName = selected.ID
		m.statusBar.UpdateModel(selected.ID)
		if selected.MaxContextWindow > 0 {
			m.contextWindow = selected.MaxContextWindow
			m.statusBar.UpdateContext(m.transcript.contextUsed, m.contextWindow)
		}
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.SwitchModelAction{
				Model:            selected.ID,
				MaxContextWindow: selected.MaxContextWindow,
				APIKey:           selected.APIKey,
				BaseURL:          selected.BaseURL,
				AuthMode:         selected.AuthMode,
				AuthHelper:       selected.AuthHelper,
				Protocol:         selected.Protocol,
				ConfigName:       selected.ConfigName,
			})
		}
		m.status = "Model switched"
	case "backspace":
		if m.modal.filter != "" {
			_, size := utf8.DecodeLastRuneInString(m.modal.filter)
			m.modal.filter = m.modal.filter[:len(m.modal.filter)-size]
			m.modal.selectedI = 0
		}
	default:
		if text := msg.Key().Text; text != "" {
			m.modal.filter += text
			m.modal.selectedI = 0
		}
	}
	return nil
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
	m.modal = modalState{kind: modalSessionPicker, sessions: sessions}
	m.status = "Resume session"
}

func (m *Model) sessionPickerView() string {
	var b strings.Builder
	b.WriteString("Resume session\n")
	if len(m.modal.sessions) == 0 {
		b.WriteString("No sessions\n")
	} else {
		for i, sess := range m.modal.sessions {
			cursor := " "
			if i == m.modal.selectedI {
				cursor = ">"
			}
			title := sess.Title
			if title == "" {
				title = sess.ID
			}
			fmt.Fprintf(&b, "%s %s  %s\n", cursor, title, sess.UpdatedAt.Format("2006-01-02 15:04"))
		}
	}
	b.WriteString("[up/down] move  [enter] load  [esc] close")
	return b.String()
}

func (m *Model) handleSessionPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.modal = modalState{}
	case "up", "ctrl+p":
		if len(m.modal.sessions) > 0 {
			m.modal.selectedI = (m.modal.selectedI - 1 + len(m.modal.sessions)) % len(m.modal.sessions)
		}
	case "down", "ctrl+n":
		if len(m.modal.sessions) > 0 {
			m.modal.selectedI = (m.modal.selectedI + 1) % len(m.modal.sessions)
		}
	case "enter", "tab":
		if len(m.modal.sessions) == 0 {
			return nil
		}
		selected := m.modal.sessions[clamp(m.modal.selectedI, 0, len(m.modal.sessions)-1)]
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.LoadSessionAction{SessionID: selected.ID})
		}
		m.status = "Loading session"
	}
	return nil
}
