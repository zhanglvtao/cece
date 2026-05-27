package ui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/skill"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

const (
	simpleInputMinHeight = 3
	simpleInputMaxHeight = 8
	modalMaxHeight       = 14
)

type globalEventMsg struct{ events []protocol.Event }
type inputErrorMsg struct{ err error }
type statusSpinnerTickMsg struct{}

// Sender submits user input to the runtime.
type Sender interface {
	Input(ctx context.Context, input string) error
}

// Actor receives fire-and-forget protocol actions from the UI.
type Actor interface {
	Do(action protocol.Action)
}

// Eventer exposes the runtime's async protocol event stream.
type Eventer interface {
	Events() <-chan protocol.Event
}

// Model is Cece's root Bubble Tea model. It intentionally keeps UI state small:
// protocol events update the transcript, and protocol actions drive the runtime.
type Model struct {
	sender Sender

	modelName           string
	mode                protocol.PermissionMode
	projectDir          string
	workDir             string
	gitBranch           string
	contextWindow       int
	status              string
	statusFrame         int
	statusSpinnerActive bool
	busy                bool
	width               int
	height              int

	streamHeadline      string // latest assistant text for inline indicator

	styles      Styles
	transcript  transcript
	viewport    viewport.Model
	input       textarea.Model
	modal       modalState
	slashPopup  *SlashPopup
	statusBar   *StatusBar

	sessions                session.Store
	currentSessionID        string
	currentSessionEphemeral bool
	skillStore              *skill.Store
	queued                  []string
	history                 []string
	historyIndex            int
}

func NewModel(sender Sender, modelName string, projectDir string, contextWindow ...int) Model {
	styles := DefaultStyles()
	input := textarea.New()
	input.Placeholder = "Send a message…"
	input.ShowLineNumbers = false
	input.CharLimit = -1
	input.DynamicHeight = true
	input.MinHeight = simpleInputMinHeight
	input.MaxHeight = simpleInputMaxHeight
	input.SetVirtualCursor(false)
	input.SetPromptFunc(0, func(textarea.PromptInfo) string { return "" })
	input.Focus()

	cw := 0
	if len(contextWindow) > 0 {
		cw = contextWindow[0]
	}

	sb := NewStatusBar()
	sb.UpdateModel(modelName)
	sb.UpdateStatus("Ready", false)
	sb.UpdateContext(0, cw)

	return Model{
		sender:        sender,
		modelName:     modelName,
		mode:          protocol.PermissionModeDefault,
		projectDir:    projectDir,
		workDir:       filepath.Base(projectDir),
		gitBranch:     gitBranch(projectDir),
		contextWindow: cw,
		status:        "Ready",
		styles:        styles,
		slashPopup:   NewSlashPopup(styles),
		transcript:   newTranscript(),
		viewport:     viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		input:        input,
		statusBar:    sb,
		historyIndex: -1,
	}
}

func (m *Model) SetSessions(store session.Store) { m.sessions = store }

func (m *Model) SetSkillStore(store *skill.Store) {
	m.skillStore = store
	if store != nil {
		m.slashPopup.SetSkills(store.All())
	}
}

func (m Model) Init() tea.Cmd {
	if eventer, ok := m.sender.(Eventer); ok {
		return consumeGlobalEventsCmd(eventer.Events())
	}
	return nil
}

func consumeGlobalEventsCmd(ch <-chan protocol.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch // block for first event
		if !ok {
			return nil
		}
		events := []protocol.Event{ev}
		// non-blocking drain remaining buffered events
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return globalEventMsg{events: events}
				}
				events = append(events, e)
			default:
				return globalEventMsg{events: events}
			}
		}
	}
}

func statusSpinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return statusSpinnerTickMsg{}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m.update(msg) }

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case inputErrorMsg:
		m.busy = false
		m.status = msg.err.Error()
		m.transcript.appendDone(blockError, "error", msg.err.Error())
		m.refreshViewport(true)
		return m, nil
	case statusSpinnerTickMsg:
		if !m.statusShowsSpinner() {
			m.statusSpinnerActive = false
			return m, nil
		}
		m.statusFrame++
		m.statusBar.TickStatusSpinner()
		return m, statusSpinnerTickCmd()
	case globalEventMsg:
		for _, ev := range msg.events {
			m.applyEvent(ev)
		}
		cmds := []tea.Cmd{}
		if cmd := m.ensureStatusSpinner(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if eventer, ok := m.sender.(Eventer); ok {
			cmds = append(cmds, consumeGlobalEventsCmd(eventer.Events()))
		}
		return m, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.checkSlashPopup()
		return m, cmd
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	if !m.modal.active() {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) applyEvent(event protocol.Event) {
	m.transcript.apply(event)
	switch e := event.(type) {
	case protocol.SessionCreated:
		m.currentSessionID = e.ID
		m.currentSessionEphemeral = true
		m.status = "Session created"
	case protocol.ModelRequestStarted:
		m.busy = true
		m.status = "Requesting"
		m.statusBar.IncrementAPICalls()
	case protocol.AssistantStarted:
		m.busy = true
		m.status = "Streaming"
		m.streamHeadline = ""
	case protocol.AssistantDelta:
		m.streamHeadline += e.Text
	case protocol.RunFailed:
		m.busy = false
		m.queued = nil
		m.status = "Failed"
		m.streamHeadline = ""
	case protocol.TurnCompleted:
		m.busy = false
		m.status = "Ready"
		m.streamHeadline = ""
	case protocol.QueuedInputPromoted:
		if len(m.queued) > 0 {
			m.queued = m.queued[1:]
		}
	case protocol.TruncationRetry:
		m.status = "Retrying"
	case protocol.ToolCallCompleted:
		m.statusBar.IncrementTool(e.Name)
	case protocol.ToolCallsReady:
		m.openToolConfirm(e.Calls)
		m.status = "Confirm tools"
	case protocol.PlanApprovalRequested:
		m.openPlanConfirm(e.PlanFile)
		m.status = "Approve plan"
	case protocol.QuestionAsked:
		m.openQuestion(e.Questions)
		m.status = "Answer question"
	case protocol.ModelsLoadedEvent:
		if e.Err != "" {
			m.status = "Failed to load models: " + e.Err
		} else {
			m.openModelPicker(e.Models)
			m.status = "Switch model"
		}
	case protocol.ModeChangedEvent:
		m.mode = e.Mode
		m.status = e.Message
		if e.Message != "" {
			m.transcript.appendDone(blockSystem, "mode", e.Message)
		}
	case protocol.ModeEvent:
		m.mode = e.Mode
	case protocol.SessionLoadedEvent:
		if e.Err != "" {
			m.status = "Failed to load session: " + e.Err
		} else {
			m.currentSessionID = e.SessionID
			m.currentSessionEphemeral = false
			if e.Model != "" {
				m.modelName = e.Model
				m.statusBar.UpdateModel(e.Model)
			}
			if e.ContextWindow > 0 {
				m.contextWindow = e.ContextWindow
			}
			m.status = "Session loaded"
		}
	case protocol.HistoryClearedEvent:
		m.transcript.reset()
		m.statusBar.ResetToolCounts()
		m.status = "Cleared"
	case protocol.CompactingEvent:
		m.status = "Compacting"
		m.busy = true
	case protocol.CompactedEvent:
		m.busy = false
		m.status = fmt.Sprintf("Compacted %d→%d msgs, %dK→%dK tokens",
			e.MessagesBefore, e.MessagesAfter,
			(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
		m.statusBar.ResetToolCounts()
	}
	// Sync all status bar data from model state.
	m.statusBar.UpdateStatus(m.status, m.busy)
	m.statusBar.UpdateTokens(m.transcript.inputTokens, m.transcript.outputTokens)
	m.statusBar.UpdateContext(m.transcript.contextUsed, m.contextWindow)
	m.refreshViewport(eventPinsViewportToBottom(event))
}

func eventPinsViewportToBottom(event protocol.Event) bool {
	switch event.(type) {
	case protocol.SessionLoadedEvent, protocol.HistoryClearedEvent:
		return true
	default:
		return false
	}
}

func (m *Model) View() tea.View {
	m.resize()
	sections := []string{m.viewport.View()}
	modal := m.modalView()
	if modal != "" {
		sections = append(sections, modal)
	}
	popup := m.slashPopup.View(m.width)
	if popup != "" {
		sections = append(sections, popup)
	}
	// Headline indicator: show latest assistant text above input during streaming
	if headline := m.headlineView(); headline != "" {
		sections = append(sections, headline)
	}
	sections = append(sections, m.inputView())
	statusBarView := m.statusBar.Render(m.width)
	if statusBarView != "" {
		sections = append(sections, statusBarView)
	}
	content := strings.Join(sections, "\n")
	view := tea.NewView(content)
	view.MouseMode = tea.MouseModeCellMotion
	view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true

	// Position cursor.
	if m.modal.kind == modalQuestion && m.modal.textMode {
		// Place cursor at the inline text input line inside the question modal.
		cur := &tea.Cursor{}
		// The input line is the last option line (before help line) in the modal.
		// Modal layout: "Question X/Y\n{question}\n{options}\n{help}"
		modalLines := strings.Count(modal, "\n") + 1
		cur.Y = m.viewport.Height() + modalLines - 2 // -1 for 0-index, -1 for help line
		cur.X = 6 + len(m.modal.textInput)            // "> [ ] " prefix (6 chars) + typed text length
		view.Cursor = cur
	} else if cur := m.input.Cursor(); cur != nil {
		rowsAboveInput := m.viewport.Height() // no header
		if modal != "" {
			rowsAboveInput += strings.Count(modal, "\n") + 1
		}
		if popup != "" {
			rowsAboveInput += strings.Count(popup, "\n") + 1
		}
		cur.Y += rowsAboveInput + m.styles.Input.Box.GetBorderTopSize() + m.styles.Input.Box.GetPaddingTop()
		cur.X += m.styles.Input.Box.GetBorderLeftSize() + m.styles.Input.Box.GetPaddingLeft()
		view.Cursor = cur
	}

	return view
}

func (m *Model) resize() {
	wasAtBottom := m.viewport.AtBottom()
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}
	modalH := m.modalHeight()
	popupH := 0
	if m.slashPopup.Active() {
		popupH = m.slashPopup.Height()
	}
	inputH := clamp(m.input.Height(), simpleInputMinHeight, simpleInputMaxHeight)
	// Update scroll cell in statusbar before layout
	if !m.viewport.AtBottom() {
		m.statusBar.UpdateScroll(int(m.viewport.ScrollPercent() * 100))
	} else {
		m.statusBar.UpdateScroll(0)
	}
	statusH := m.statusBar.Height()
	vFrame := m.styles.Input.Box.GetVerticalFrameSize()
	hFrame := m.styles.Input.Box.GetHorizontalFrameSize()
	headlineH := 0
	if m.busy && m.streamHeadline != "" {
		headlineH = 1
	}
	viewportH := m.height - modalH - popupH - inputH - vFrame - statusH - headlineH
	if viewportH < 3 {
		viewportH = 3
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(viewportH)
	m.input.SetWidth(max(1, m.width-hFrame))
	m.input.SetHeight(inputH)
	m.refreshViewport(wasAtBottom)
}

func (m *Model) refreshViewport(gotoBottom bool) {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.transcript.render(m.width, m.styles))
	if gotoBottom || atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) inputView() string {
	return m.styles.Input.Box.Width(m.width).Render(m.input.View())
}

// headlineView renders a one-line indicator above the input box showing the
// latest assistant text delta while streaming. Includes a rotating spinner.
func (m *Model) headlineView() string {
	if !m.busy || m.streamHeadline == "" {
		return ""
	}
	frame := string(statusSpinnerFrames[m.statusFrame%len(statusSpinnerFrames)])
	// Take the last non-empty line of accumulated text
	text := m.streamHeadline
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	text = lines[len(lines)-1]
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Truncate to fit
	maxLen := m.width - 4 // spinner + spaces
	if maxLen < 10 {
		maxLen = 10
	}
	if len(text) > maxLen {
		text = text[:maxLen-3] + "..."
	}
	return m.styles.Status.Render(frame + " " + text)
}

func (m *Model) statusShowsSpinner() bool {
	return m.status == "Requesting" || m.status == "Streaming"
}

func (m *Model) ensureStatusSpinner() tea.Cmd {
	if !m.statusShowsSpinner() {
		m.statusSpinnerActive = false
		return nil
	}
	if m.statusSpinnerActive {
		return nil
	}
	m.statusSpinnerActive = true
	return statusSpinnerTickCmd()
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if handled := m.handleChatScrollKey(msg); handled {
		return m, nil
	}
	if m.modal.active() {
		return m, m.handleModalKey(msg)
	}

	// Slash popup key handling takes priority when active.
	if m.slashPopup.Active() {
		return m.handleSlashPopupKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.busy {
			m.cancelTurn("Cancelled")
			return m, nil
		}
		return m, func() tea.Msg { return tea.Quit() }
	case "esc":
		if m.busy {
			m.cancelTurn("Cancelled")
		}
		return m, nil
	case "enter":
		return m, m.handleSend()
	case "ctrl+j", "shift+enter":
		m.input.InsertRune('\n')
		return m, nil
	case "shift+tab", "backtab":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.CyclePermissionModeAction{})
		}
		return m, nil
	case "up":
		if m.inputAtStart() && m.historyPrev() {
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" {
			m.viewport.ScrollUp(1)
			return m, nil
		}
	case "down":
		if m.inputAtEnd() && m.historyNext() {
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" {
			m.viewport.ScrollDown(1)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// After any input change, check if we should open the slash popup.
	m.checkSlashPopup()

	return m, cmd
}

func (m *Model) handleSlashPopupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "up":
		m.slashPopup.SelectUp()
		return m, nil
	case "down":
		m.slashPopup.SelectDown()
		return m, nil
	case "esc":
		m.slashPopup.Close()
		return m, nil
	case "tab", "enter":
		if cmd, ok := m.slashPopup.SelectedCommand(); ok {
			m.input.SetValue(cmd + " ")
			m.input.CursorEnd()
			m.slashPopup.Close()
		}
		return m, nil
	case "space":
		m.slashPopup.Close()
		// Fall through to insert space into textarea.
	}

	// For all other keys (including backspace, printable chars), pass to textarea.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Update popup filter or close if no longer matching.
	spec := parseSlashSpec(m.input.Value())
	if !spec.Active {
		m.slashPopup.Close()
	} else if spec.HasArgs {
		m.slashPopup.Close()
	} else {
		m.slashPopup.UpdateFilter(spec.Query)
	}

	return m, cmd
}

// checkSlashPopup opens the slash popup when the input starts with "/".
func (m *Model) checkSlashPopup() {
	spec := parseSlashSpec(m.input.Value())
	if spec.Active && !spec.HasArgs {
		if !m.slashPopup.Active() {
			m.slashPopup.Open(spec.Query)
		} else {
			m.slashPopup.UpdateFilter(spec.Query)
		}
	}
}

func (m *Model) handleChatScrollKey(msg tea.KeyPressMsg) bool {
	key := msg.Key()
	switch key.Code {
	case tea.KeyPgUp:
		m.viewport.ScrollUp(max(1, m.viewport.Height()-1))
		return true
	case tea.KeyPgDown:
		m.viewport.ScrollDown(max(1, m.viewport.Height()-1))
		return true
	case tea.KeyHome:
		m.viewport.GotoTop()
		return true
	case tea.KeyEnd:
		m.viewport.GotoBottom()
		return true
	}
	if !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		return false
	}
	switch key.Code {
	case tea.KeyUp, tea.KeyKpUp:
		m.viewport.ScrollUp(1)
		return true
	case tea.KeyDown, tea.KeyKpDown:
		m.viewport.ScrollDown(1)
		return true
	default:
		return false
	}
}

func (m *Model) handleSend() tea.Cmd {
	input := strings.TrimSpace(m.input.Value())
	if input == "" {
		return nil
	}
	m.input.Reset()
	m.addHistory(input)
	m.viewport.GotoBottom()
	if strings.HasPrefix(strings.TrimLeft(input, " \t"), "/") {
		return m.handleSlashCommand(input)
	}
	if m.busy {
		m.queueInput(input)
		return nil
	}
	m.busy = true
	m.status = "Submitting"
	return submitCmd(m.sender, input)
}

func (m *Model) handleSlashCommand(input string) tea.Cmd {
	spec := parseSlashSpec(input)
	switch spec.Command {
	case "/model":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ListModelsAction{})
			m.status = "Loading models"
		}
		return nil
	case "/resume":
		m.openSessionsDialog()
		return nil
	case "/clear":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ClearHistoryAction{})
			m.status = "Cleared"
		}
		return nil
	case "/compact":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.CompactAction{})
			m.status = "Compacting"
			m.busy = true
		}
		return nil
	case "/skills":
		if m.skillStore != nil {
			m.transcript.appendDone(blockInfo, "skills", skill.FormatSkillList(m.skillStore.All()))
			m.status = "Skills listed"
		}
		return nil
	}
	name := strings.TrimPrefix(spec.Command, "/")
	if m.skillStore != nil {
		if sk, ok := m.skillStore.Get(name); ok {
			content := skill.FormatInvocation(sk, spec.Args)
			if m.busy {
				m.queueInput(content)
				return nil
			}
			m.busy = true
			m.status = "Submitting skill"
			return submitCmd(m.sender, content)
		}
	}
	m.status = formatSlashUnknown(spec.Command)
	return nil
}

func (m *Model) queueInput(input string) {
	if actor, ok := m.sender.(Actor); ok {
		actor.Do(protocol.QueueInputAction{Text: input})
	}
	m.queued = append(m.queued, input)
	m.status = fmt.Sprintf("Queued (%d)", len(m.queued))
}

func (m *Model) cancelTurn(status string) {
	if actor, ok := m.sender.(Actor); ok {
		actor.Do(protocol.CancelAction{})
	}
	m.busy = false
	m.queued = nil
	m.status = status
}

func (m *Model) addHistory(input string) {
	if input == "" {
		return
	}
	if len(m.history) == 0 || m.history[0] != input {
		m.history = append([]string{input}, m.history...)
	}
	m.historyIndex = -1
}

func (m *Model) historyPrev() bool {
	if len(m.history) == 0 {
		return false
	}
	next := m.historyIndex + 1
	if next >= len(m.history) {
		return false
	}
	m.historyIndex = next
	m.input.SetValue(m.history[next])
	return true
}

func (m *Model) historyNext() bool {
	if m.historyIndex < 0 {
		return false
	}
	next := m.historyIndex - 1
	m.historyIndex = next
	if next < 0 {
		m.input.SetValue("")
	} else {
		m.input.SetValue(m.history[next])
	}
	return true
}

func (m *Model) inputAtStart() bool { return m.input.Line() == 0 }

func (m *Model) inputAtEnd() bool { return m.input.Line() >= strings.Count(m.input.Value(), "\n") }

func submitCmd(sender Sender, input string) tea.Cmd {
	return func() tea.Msg {
		if sender == nil {
			return inputErrorMsg{err: fmt.Errorf("runtime unavailable")}
		}
		if err := sender.Input(context.Background(), input); err != nil {
			return inputErrorMsg{err: err}
		}
		return nil
	}
}

func gitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
