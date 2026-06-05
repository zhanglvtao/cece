package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"cece/internal/config"
	"cece/internal/logger"
	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/skill"
	"cece/internal/ui/picker"
	"cece/internal/ui/theme"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// styledPickerItem renders a picker item with colored cursor and dimmed non-selected text.
func styledPickerItem(cursorStyle lipgloss.Style, itemStyle lipgloss.Style, text string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("> ")
	} else {
		text = itemStyle.Render(text)
	}
	return cursor + text
}

type modalKind int

const (
	modalNone modalKind = iota
	modalConfirmTools
	modalApprovePlan
	modalQuestion
	modalModelPicker
	modalSessionPicker
	modalMCPPicker
	modalSkillPicker
	modalRenameSession
)

type modalState struct {
	kind          modalKind
	calls         []protocol.ToolUseBlock
	questions     []protocol.Question
	qIndex        int
	cursors       []int
	selected      map[int][]int
	custom        map[int]string
	textMode      bool
	textInput     string
	picker        *picker.Picker
	planFile      string
	skillEnabled  map[string]bool // skill name -> enabled state in picker
}

func (m modalState) active() bool { return m.kind != modalNone }

func (m *Model) modalHeight() int {
	if !m.modal.active() {
		return 0
	}
	if m.modal.kind == modalModelPicker || m.modal.kind == modalSessionPicker || m.modal.kind == modalMCPPicker || m.modal.kind == modalSkillPicker {
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
		body = m.styles.Modal.Title.Render("Approve plan "+m.modal.planFile+"?") + "\n" + m.styles.Modal.Help.Render("[y/enter] approve  [shift+tab] auto-accept  [n/esc] reject")
	case modalQuestion:
		body = m.questionView()
	case modalModelPicker, modalSessionPicker, modalMCPPicker, modalSkillPicker:
		if m.modal.picker != nil {
			body = m.modal.picker.View()
		}
	case modalRenameSession:
		body = m.renameSessionView()
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
	case modalModelPicker, modalSessionPicker, modalMCPPicker:
		return m.handlePickerKey(msg)
	case modalSkillPicker:
		return m.handleSkillPickerKey(msg)
	case modalRenameSession:
		return m.handleRenameSessionKey(msg)
	}
	return nil
}

func (m *Model) openToolConfirm(calls []protocol.ToolUseBlock) {
	m.modal = modalState{kind: modalConfirmTools, calls: calls}
}

func (m *Model) confirmToolsView() string {
	var b strings.Builder
	b.WriteString(m.styles.Modal.Title.Render("Allow tool calls?") + "\n")
	for _, call := range m.modal.calls {
		args := formatJSONPreview(call.Input)
		args = strings.ReplaceAll(args, "\n", " ")
		b.WriteString("- " + m.styles.Modal.Tool.Render(call.Name))
		if args != "" {
			b.WriteString("  " + m.styles.Modal.ToolArg.Render(ansi.Truncate(args, max(20, m.width-20), "...")))
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.styles.Modal.Help.Render("[y/enter] allow  [shift+tab] auto-accept  [n/esc] deny"))
	return b.String()
}

func (m *Model) handleConfirmToolsKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y", "enter":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ConfirmAction{})
		}
	case "shift+tab", "backtab":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionModeAutoAccept})
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
	case "shift+tab", "backtab":
		m.modal = modalState{}
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.SetExitTargetModeAction{Mode: protocol.PermissionModeAutoAccept})
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
		return m.styles.Modal.Title.Render("Question") + "\n" + m.styles.Modal.Help.Render("[esc] cancel")
	}
	q := m.modal.questions[m.modal.qIndex]
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d/%d\n%s\n", m.styles.Modal.Title.Render("Question"), m.modal.qIndex+1, len(m.modal.questions), q.Question)
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
				displayLabel := m.modal.textInput + "▌"
				if m.modal.textInput == "" {
					displayLabel = "▌"
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
			if q.Options[i].Description != "" {
				fmt.Fprintf(&b, "      %s\n", q.Options[i].Description)
			}
		}
	}
	b.WriteString(m.styles.Modal.Help.Render("[up/down] move  [space] toggle  [enter] next  [shift+tab] auto-answer  [esc] cancel"))
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
			if text := msg.Key().Text; text != "" && !csiResidueRe.MatchString(text) {
				m.modal.textInput += text
			}
		}
		return nil
	}
	optionCount := len(q.Options) + 1
	cursor := m.modal.cursors[m.modal.qIndex]
	// If cursor is on "Type something else..." and user types a printable
	// character, enter text mode immediately and append the character.
	if cursor == len(q.Options) {
		if text := msg.Key().Text; text != "" && !csiResidueRe.MatchString(text) {
			m.modal.textMode = true
			m.modal.textInput = text
			return nil
		}
	}
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
		if cursor < len(q.Options) {
			m.toggleQuestionSelection(m.modal.qIndex, cursor)
		}
	case "enter":
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
	case "shift+tab", "backtab":
		// Auto-answer: select current cursor option for all questions, then submit.
		for i, q := range m.modal.questions {
			cursor := m.modal.cursors[i]
			if cursor < len(q.Options) {
				if !m.questionSelected(i, cursor) {
					m.toggleQuestionSelection(i, cursor)
				}
			}
		}
		m.modal.qIndex = len(m.modal.questions) - 1
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
		return styledPickerItem(m.styles.Picker.Cursor, m.styles.Picker.Item, provider+name, selected)
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
			logger.Info("UI: contextWindow changed by model picker", "old", m.contextWindow, "new", mi.MaxContextWindow, "model", mi.ID)
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

		model := s.ConfigName
		if model != "" {
			model += "/"
		}
		model += s.Model
		if s.Model == "" {
			model = ""
		}

		timeStr := s.UpdatedAt.Format("01-02 15:04")

		msgStr := ""
		if s.MessageCount > 0 {
			msgStr = fmt.Sprintf("%d msg", s.MessageCount)
		}

		// Build line 1 with aligned columns
		const modelColW = 18
		const timeColW = 12
		const msgColW = 7
		rightWidth := modelColW + timeColW + msgColW + 6
		titleBudget := max(10, m.width-rightWidth-2)

		if lipgloss.Width(title) > titleBudget {
			title = ansi.Truncate(title, titleBudget, "…")
		}
		titlePad := titleBudget - lipgloss.Width(title)

		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Fg)
		titleDimStyle := lipgloss.NewStyle().Foreground(theme.FgSubtle)
		infoStyle := m.styles.Picker.Info
		msgStyle := lipgloss.NewStyle().Foreground(theme.Green)

		var b strings.Builder
		if selected {
			b.WriteString(titleStyle.Render(title))
		} else {
			b.WriteString(titleDimStyle.Render(title))
		}
		b.WriteString(strings.Repeat(" ", titlePad))

		if model != "" {
			b.WriteString("  " + infoStyle.Render(padRight(model, modelColW)))
		} else {
			b.WriteString("  " + strings.Repeat(" ", modelColW))
		}
		b.WriteString("  " + infoStyle.Render(padRight(timeStr, timeColW)))
		if msgStr != "" {
			b.WriteString("  " + msgStyle.Render(padLeft(msgStr, msgColW)))
		}

		cursorStyle := m.styles.Picker.Cursor
		line1 := cursorStyle.Render("> ") + b.String()
		if !selected {
			line1 = "  " + b.String()
		}

		if s.Preview != "" {
			return line1 + "\n" + m.styles.Picker.Preview.Render("  "+s.Preview)
		}
		return line1
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

func (m *Model) openRenameSessionDialog() bool {
	if m.currentSessionID == "" || m.sessions == nil {
		return false
	}
	sess, err := m.sessions.Get(context.Background(), m.currentSessionID)
	if err != nil || sess == nil {
		return false
	}
	title := sess.Title
	if title == "" {
		title = sess.ID
	}
	m.modal = modalState{
		kind:      modalRenameSession,
		textInput: title,
	}
	return true
}

func (m *Model) renameSessionView() string {
	return fmt.Sprintf("Rename session (ctrl+c/enter to save & quit, esc to quit):\n> %s▌", m.modal.textInput)
}

func (m *Model) openMCPPicker(servers []protocol.MCPServerInfo) {
	if len(servers) == 0 {
		m.transcript.appendDone(blockInfo, "mcp", "No MCP servers configured. Add servers to .cece/settings.json under \"mcp\".")
		return
	}
	items := make([]any, len(servers))
	for i, s := range servers {
		items[i] = s
	}
	p := picker.New("MCP servers", items, modalMaxHeight, func(item any, selected bool) string {
		s := item.(protocol.MCPServerInfo)
		status := "disconnected"
		if s.Connected {
			status = fmt.Sprintf("connected (%d tools)", s.ToolCount)
		} else if s.Error != "" {
			status = s.Error
		}
		text := fmt.Sprintf("%s  %s  %s", s.Name, s.Type, status)
		return styledPickerItem(m.styles.Picker.Cursor, m.styles.Picker.Item, text, selected)
	})
	p.SetHelpText("[up/down] move  [enter] toggle connect/disconnect  [esc] close")
	p.SetOnSelect(func(item any) tea.Cmd {
		s := item.(protocol.MCPServerInfo)
		if actor, ok := m.sender.(Actor); ok {
			if s.Connected {
				actor.Do(protocol.DisconnectMCPAction{Name: s.Name})
			} else {
				actor.Do(protocol.ConnectMCPAction{Name: s.Name})
			}
		}
		return nil
	})
	m.modal = modalState{kind: modalMCPPicker, picker: p}
}

func (m *Model) openSkillPicker() {
	if m.skillStore == nil {
		return
	}
	allSkills := m.skillStore.All()
	if len(allSkills) == 0 {
		m.transcript.appendDone(blockInfo, "skills", "No skills found.")
		return
	}

	// Build enabled map from current store state
	skillEnabled := make(map[string]bool, len(allSkills))
	for _, s := range allSkills {
		skillEnabled[s.Name] = m.skillStore.IsEnabled(s.Name)
	}

	items := make([]any, len(allSkills))
	for i, s := range allSkills {
		items[i] = s
	}

	p := picker.New("Skills", items, modalMaxHeight, func(item any, selected bool) string {
		sk := item.(*skill.Skill)
		mark := "[ ]"
		if skillEnabled[sk.Name] {
			mark = "[✓]"
		}
		source := sk.Source
		text := fmt.Sprintf("%s %s  %s  %s", mark, sk.Name, source, sk.Description)
		return styledPickerItem(m.styles.Picker.Cursor, m.styles.Picker.Item, text, selected)
	})
	p.SetHelpText("[up/down] move  [space] toggle  [enter/esc] close")
	p.SetFilterFn(func(item any, q string) bool {
		sk := item.(*skill.Skill)
		return containsFold(sk.Name+" "+sk.Description, q)
	})

	m.modal = modalState{
		kind:         modalSkillPicker,
		picker:       p,
		skillEnabled: skillEnabled,
	}
}

func (m *Model) handleSkillPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.modal.picker == nil {
		return nil
	}

	switch msg.String() {
	case "space":
		// Toggle the currently selected skill
		item := m.modal.picker.Selected()
		if item == nil {
			return nil
		}
		sk, ok := item.(*skill.Skill)
		if !ok {
			return nil
		}
		m.modal.skillEnabled[sk.Name] = !m.modal.skillEnabled[sk.Name]
		// Apply to store and slash popup immediately
		m.applySkillPickerState()
		// Rebuild picker to update checkboxes
		m.rebuildSkillPicker()
		return nil
	case "enter", "esc":
		// Persist and close
		m.closeSkillPicker()
		return nil
	default:
		// Delegate to picker for navigation and filtering
		result, cmd := m.modal.picker.HandleKey(msg)
		if result == picker.ResultClose {
			m.closeSkillPicker()
		}
		return cmd
	}
}

// rebuildSkillPicker rebuilds the picker with updated checkbox states.
func (m *Model) rebuildSkillPicker() {
	allSkills := m.skillStore.All()
	items := make([]any, len(allSkills))
	for i, s := range allSkills {
		items[i] = s
	}
	skillEnabled := m.modal.skillEnabled
	selectedIdx := m.modal.picker.SelectedIdx()

	p := picker.New("Skills", items, modalMaxHeight, func(item any, selected bool) string {
		sk := item.(*skill.Skill)
		mark := "[ ]"
		if skillEnabled[sk.Name] {
			mark = "[✓]"
		}
		text := fmt.Sprintf("%s %s  %s  %s", mark, sk.Name, sk.Source, sk.Description)
		return styledPickerItem(m.styles.Picker.Cursor, m.styles.Picker.Item, text, selected)
	})
	p.SetHelpText("[up/down] move  [space] toggle  [enter/esc] close")
	p.SetFilterFn(func(item any, q string) bool {
		sk := item.(*skill.Skill)
		return containsFold(sk.Name+" "+sk.Description, q)
	})

	// Restore selection position
	for i := 0; i < selectedIdx && i < len(items); i++ {
		p.Down()
	}

	m.modal.picker = p
}

// applySkillPickerState syncs the modal's skill enabled map to the store
// and slash popup, for real-time effect during the picker session.
func (m *Model) applySkillPickerState() {
	var enabledNames []string
	for _, sk := range m.skillStore.All() {
		if m.modal.skillEnabled[sk.Name] {
			enabledNames = append(enabledNames, sk.Name)
		}
	}
	m.skillStore.SetEnabled(enabledNames)
	m.slashPopup.SetSkills(m.skillStore.Enabled())
}

// closeSkillPicker persists the enabled state and closes the modal.
func (m *Model) closeSkillPicker() {
	m.applySkillPickerState()

	// Persist to settings.json
	if m.projectDir != "" {
		enabledNames := m.skillStore.EnabledNames()
		if err := config.SaveEnabledSkills(m.projectDir, enabledNames); err != nil {
			m.status = "Failed to save skills: " + err.Error()
		} else {
			m.status = "Skills saved"
		}
	}

	m.modal = modalState{}
}

func (m *Model) showToolList(tools []protocol.ToolInfo) {
	if len(tools) == 0 {
		m.transcript.appendDone(blockInfo, "tools", "No tools registered.")
		return
	}

	// Group by source
	groups := make(map[string][]protocol.ToolInfo)
	var order []string
	for _, t := range tools {
		if _, ok := groups[t.Source]; !ok {
			order = append(order, t.Source)
		}
		groups[t.Source] = append(groups[t.Source], t)
	}

	var b strings.Builder
	for _, source := range order {
		label := source
		if source == "builtin" {
			label = "Built-in tools"
		} else if strings.HasPrefix(source, "mcp:") {
			label = "MCP tools (" + strings.TrimPrefix(source, "mcp:") + ")"
		}
		b.WriteString(label + ":\n")
		for _, t := range groups[source] {
			desc := t.Description
			if desc != "" {
				if len(desc) > 80 {
					desc = desc[:77] + "..."
				}
				b.WriteString(fmt.Sprintf("  %s — %s\n", t.Name, desc))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", t.Name))
			}
		}
	}
	m.transcript.appendDone(blockInfo, "tools", b.String())
}

func (m *Model) handleRenameSessionKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "enter":
		newTitle := strings.TrimSpace(m.modal.textInput)
		sessionID := m.currentSessionID
		m.modal = modalState{}
		if newTitle != "" && sessionID != "" {
			if actor, ok := m.sender.(Actor); ok {
				actor.Do(protocol.RenameSessionAction{SessionID: sessionID, Title: newTitle})
			}
		}
		return func() tea.Msg { return tea.Quit() }
	case "esc":
		m.modal = modalState{}
		return func() tea.Msg { return tea.Quit() }
	case "backspace":
		if m.modal.textInput != "" {
			_, size := utf8.DecodeLastRuneInString(m.modal.textInput)
			m.modal.textInput = m.modal.textInput[:len(m.modal.textInput)-size]
		}
	default:
		if text := msg.Key().Text; text != "" && !csiResidueRe.MatchString(text) {
			m.modal.textInput += text
		}
	}
	return nil
}

// padRight pads s with spaces on the right to reach target visual width.
func padRight(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// padLeft pads s with spaces on the left to reach target visual width.
func padLeft(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return strings.Repeat(" ", gap) + s
}
