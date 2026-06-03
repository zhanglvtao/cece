package ui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"cece/internal/logger"
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

	streamHeadline string // latest assistant text for inline indicator
	tasks          []protocol.TodoItem
	runningAgents  []runningAgent

	styles     Styles
	transcript transcript
	viewport   viewport.Model
	input      textarea.Model
	modal      modalState
	slashPopup *SlashPopup
	filePopup  *FilePopup
	statusBar  *StatusBar

	sessions                session.Store
	currentSessionID        string
	currentSessionEphemeral bool
	pendingQuit             bool // set on ctrl+c, quit after title generation completes
	shouldQuit              bool // set by applyEvent when pendingQuit title is done
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
	sb.UpdateMode(string(protocol.PermissionModeDefault))
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
		slashPopup:    NewSlashPopup(styles),
		filePopup:     NewFilePopup(projectDir),
		transcript:    newTranscript(),
		viewport:      viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		input:         input,
		statusBar:     sb,
		historyIndex:  -1,
	}
}

func (m *Model) SetSessions(store session.Store) { m.sessions = store }

// SetDefaultMode sets the initial permission mode from config.
func (m *Model) SetDefaultMode(mode string) {
	if mode != "" {
		m.mode = protocol.PermissionMode(mode)
	}
	if m.statusBar != nil {
		m.statusBar.UpdateMode(string(m.mode))
	}
}

func (m *Model) SetSkillStore(store *skill.Store) {
	m.skillStore = store
	if store != nil {
		m.slashPopup.SetSkills(store.All())
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{func() tea.Msg { return tea.RequestBackgroundColor() }}
	if eventer, ok := m.sender.(Eventer); ok {
		cmds = append(cmds, consumeGlobalEventsCmd(eventer.Events()))
	}
	return tea.Batch(cmds...)
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
	case tea.BackgroundColorMsg:
		m.styles = DefaultStyles()
		invalidateMarkdownCache()
		m.refreshViewport(false)
		return m, nil
	case inputErrorMsg:
		m.busy = false
		errMsg := appendErrorContext(msg.err.Error())
		m.status = msg.err.Error()
		m.transcript.appendDone(blockError, "error", errMsg)
		m.refreshViewport(true)
		return m, nil
	case statusSpinnerTickMsg:
		if !m.statusShowsSpinner() && !m.hasInProgressTask() && len(m.runningAgents) == 0 {
			m.statusSpinnerActive = false
			return m, nil
		}
		m.statusFrame++
		m.statusBar.TickStatusSpinner()
		return m, statusSpinnerTickCmd()
	case filesLoadedMsg:
		m.filePopup.OnFilesLoaded(msg.root)
		return m, nil
	case globalEventMsg:
		for _, ev := range msg.events {
			m.applyEvent(ev)
		}
		if m.shouldQuit {
			m.shouldQuit = false
			return m, func() tea.Msg { return tea.Quit() }
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
		msg.Content = sanitizePasteContent(msg.Content)
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.checkSlashPopupActive()
		m.filePopup.Close()
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
	case protocol.SessionTitleGeneratedEvent:
		if e.Err != "" {
			m.status = errorStatus("Title generation failed")
		} else {
			m.status = "Title: " + e.Title
			if e.SessionID == m.currentSessionID {
				m.currentSessionEphemeral = false
			}
		}
		if m.pendingQuit {
			m.shouldQuit = true
			m.pendingQuit = false
		}
	case protocol.ModelRequestStarted:
		m.busy = true
		m.status = "Requesting"
		m.statusBar.SetAPICalls(e.APICalls)
	case protocol.AssistantStarted:
		m.busy = true
		m.status = "Streaming"
		m.streamHeadline = ""
	case protocol.AssistantDelta:
		m.streamHeadline += e.Text
	case protocol.RunFailed:
		m.busy = false
		m.queued = nil
		m.status = errorStatus("Failed")
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
		// tool count is set from ToolExecCompleted
	case protocol.ToolExecCompleted:
		m.statusBar.SetToolCounts(e.ToolCounts)
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
			m.status = errorStatus("Failed to load models: " + e.Err)
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
			m.status = errorStatus("Failed to load session: " + e.Err)
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
			m.statusBar.Restore(e.APICalls, e.ToolCounts, e.CacheReadTokens, e.CacheCreationTokens)
			m.status = "Session loaded"
		}
	case protocol.HistoryClearedEvent:
		m.transcript.reset()
		m.status = "Cleared"
	case protocol.CompactingEvent:
		m.status = "Compacting"
	case protocol.CompactedEvent:
		if e.MessagesBefore == e.MessagesAfter {
			m.status = "Not enough messages to compact"
			m.transcript.appendDone(blockInfo, "compact", "Not enough messages to compact. Send a few more messages first.")
		} else {
			m.status = fmt.Sprintf("Compacted %d→%d msgs, %dK→%dK tokens",
				e.MessagesBefore, e.MessagesAfter,
				(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
			m.transcript.appendDone(blockInfo, "compact", e.Summary)
			m.transcript.contextUsed = e.TokensAfter
		}
		m.statusBar.ResetToolCounts()
	case protocol.TruncatedToolResultsEvent:
		m.status = fmt.Sprintf("Truncated %d tool results, %dK→%dK tokens",
			e.TruncatedCount,
			(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
	case protocol.PrunedEvent:
		m.status = fmt.Sprintf("Pruned %d turns, %dK→%dK tokens",
			e.PrunedTurns,
			(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
		m.statusBar.ResetToolCounts()
	case protocol.ContextNudgedEvent:
		m.status = fmt.Sprintf("Context nudge: %d%% used, %d turns since compact", e.ContextPct, e.TurnsSinceCompact)

	case protocol.MCPServersListedEvent:
		m.openMCPPicker(e.Servers)
		m.status = "MCP servers"
	case protocol.MCPServerStatusChangedEvent:
		if e.Error != "" {
			m.status = errorStatus(fmt.Sprintf("MCP %s: %s", e.Name, e.Error))
		} else if e.Connected {
			m.status = fmt.Sprintf("MCP %s: connected", e.Name)
		} else {
			m.status = fmt.Sprintf("MCP %s: disconnected", e.Name)
		}
		m.transcript.appendDone(blockInfo, "mcp", m.status)
	case protocol.ToolsListedEvent:
		m.showToolList(e.Tools)
		m.status = "Tools listed"
	case protocol.RequestDryRunEvent:
		m.status = "Dry run ready"
	case protocol.TodoUpdatedEvent:
		m.tasks = e.Tasks
	case protocol.SubAgentStartedEvent:
		m.upsertRunningAgent(e.ID, e.Description)
	case protocol.SubAgentActivityEvent:
		m.updateRunningAgentActivity(e.ID, e.Activity)
	case protocol.SubAgentCompletedEvent:
		m.removeRunningAgent(e.ID)
	case protocol.SubAgentFailedEvent:
		m.removeRunningAgent(e.ID)
		m.status = errorStatus(fmt.Sprintf("● %s failed: %s", e.Description, e.Error))
	}
	// Sync all status bar data from model state.
	m.statusBar.UpdateMode(string(m.mode))
	m.statusBar.UpdateStatus(m.status, m.busy)
	m.statusBar.UpdateTokens(m.transcript.inputTokens, m.transcript.outputTokens)
	m.statusBar.UpdateContext(m.transcript.contextUsed, m.contextWindow)
	m.statusBar.UpdateCache(m.transcript.cacheReadTokens, m.transcript.cacheCreationTokens)
	m.refreshViewport(eventPinsViewportToBottom(event))
}

// errorStatus prefixes a status message with the current session ID.
func errorStatus(msg string) string {
	sid := logger.GetSessionID()
	if sid == "" {
		return msg
	}
	return fmt.Sprintf("[%s] %s", sid, msg)
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
	sep := m.styles.Status.Separator.Render(strings.Repeat("─", max(m.width, 0)))
	sections := []string{m.viewport.View()}
	modal := m.modalView()
	if modal != "" {
		sections = append(sections, sep)
		sections = append(sections, modal)
	}
	// Task bar: show tasks above headline when active
	taskBar := m.taskBarView()
	agentBar := m.agentBarView()
	headline := m.headlineView()
	queued := m.queuedListView()
	// Task bar: bordered block with label
	if taskBar != "" {
		sections = append(sections, sep)
		sections = append(sections, taskBar)
		sections = append(sections, sep)
	} else if agentBar != "" || headline != "" || queued != "" {
		sections = append(sections, sep)
	}
	if agentBar != "" {
		sections = append(sections, agentBar)
	}
	if headline != "" {
		sections = append(sections, headline)
	}
	if queued != "" {
		sections = append(sections, queued)
	}
	// Popups must be directly above input box
	popup := m.slashPopup.View(m.width)
	if popup != "" {
		sections = append(sections, popup)
	}
	filePopupView := m.filePopup.View(m.width)
	if filePopupView != "" {
		sections = append(sections, filePopupView)
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
		cur.Y = m.viewport.Height() + modalLines - 2      // -1 for 0-index, -1 for help line
		cur.X = 6 + uniseg.StringWidth(m.modal.textInput) // "> [ ] " prefix (6 chars) + typed text display width
		view.Cursor = cur
	} else if cur := m.input.Cursor(); cur != nil {
		rowsAboveInput := m.viewport.Height() // no header
		if modal != "" {
			rowsAboveInput += 1 + strings.Count(modal, "\n") + 1 // sep + modal
		}
		if popup != "" {
			rowsAboveInput += strings.Count(popup, "\n") + 1
		}
		if filePopupView != "" {
			rowsAboveInput += strings.Count(filePopupView, "\n") + 1
		}
		if taskBar != "" {
			rowsAboveInput += 1 + strings.Count(taskBar, "\n") + 1 + 1 // sep + taskBar + sep
		} else if agentBar != "" || headline != "" || queued != "" {
			rowsAboveInput++ // separator line
		}
		if agentBar != "" {
			rowsAboveInput += strings.Count(agentBar, "\n") + 1
		}
		if headline != "" {
			rowsAboveInput += strings.Count(headline, "\n") + 1
		}
		if queued != "" {
			rowsAboveInput += strings.Count(queued, "\n") + 1
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
	if m.filePopup.Active() {
		popupH += m.filePopup.Height()
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
	if m.status != "" {
		headlineH = 2 // headline(1) + blank separator between viewport and headline(1)
	}
	taskBarH := m.taskBarHeight()
	agentBarH := m.agentBarHeight()
	viewportH := m.height - modalH - popupH - inputH - vFrame - statusH - headlineH - taskBarH - agentBarH
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

// queuedListView renders the queued user messages above the input box.
// Each message is shown on its own line with a "• " prefix.
// Plain text only — no lipgloss styling.
func (m *Model) queuedListView() string {
	if len(m.queued) == 0 {
		return ""
	}
	var b strings.Builder
	for i, msg := range m.queued {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("• ")
		b.WriteString(msg)
	}
	return b.String()
}

func (m *Model) inputView() string {
	return m.styles.Input.Box.Width(m.width).Render(m.input.View())
}

// headlineView renders a one-line indicator above the input box.
// Shows "<spinner> <status>" when idle (e.g. "- Ready"),
// and "<spinner> <status> | <streamHeadline>" when busy streaming.
// No lipgloss styling — plain text only.
func (m *Model) headlineView() string {
	if m.status == "" {
		return ""
	}
	// Build the status prefix with spinner
	statusText := m.status
	if m.statusShowsSpinner() {
		frame := string(statusSpinnerFrames[m.statusFrame%len(statusSpinnerFrames)])
		statusText = frame + " " + m.status
	}
	// Colorize the status portion
	prefix := m.styles.Headline.Render(statusText)
	// Append streamHeadline if present, separated by " | "
	if m.busy && m.streamHeadline != "" {
		text := m.streamHeadline
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		text = lines[len(lines)-1]
		text = strings.TrimSpace(text)
		if text != "" {
			prefix += " | " + text
		}
	}
	// Truncate to fit
	maxLen := m.width
	if maxLen < 10 {
		maxLen = 10
	}
	if len(prefix) > maxLen {
		prefix = prefix[:maxLen-3] + "..."
	}
	return prefix
}

func (m *Model) statusShowsSpinner() bool {
	return strings.HasSuffix(m.status, "ing")
}

func (m *Model) ensureStatusSpinner() tea.Cmd {
	if !m.statusShowsSpinner() && !m.hasInProgressTask() && len(m.runningAgents) == 0 {
		m.statusSpinnerActive = false
		return nil
	}
	if m.statusSpinnerActive {
		return nil
	}
	m.statusSpinnerActive = true
	return statusSpinnerTickCmd()
}

func (m *Model) hasInProgressTask() bool {
	for _, t := range m.tasks {
		if t.Status == "in_progress" {
			return true
		}
	}
	return false
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

	// File popup key handling when active.
	if m.filePopup.Active() {
		return m.handleFilePopupKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		if m.busy {
			m.cancelTurn("Cancelled")
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			m.input.Reset()
			m.slashPopup.Close()
			m.filePopup.Close()
			return m, nil
		}
		// Input is empty — request auto-title then quit after it completes.
		if m.currentSessionID != "" {
			if actor, ok := m.sender.(Actor); ok {
				actor.Do(protocol.AutoTitleSessionAction{SessionID: m.currentSessionID})
				m.pendingQuit = true
				m.status = "Generating title…"
				return m, nil
			}
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
		if strings.TrimSpace(m.input.Value()) == "" && len(m.queued) > 0 {
			m.dequeueLast()
			return m, nil
		}
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

	// After any input change, check if we should open the slash popup or file popup.
	if slashCmd := m.checkSlashPopup(msg); slashCmd != nil {
		return m, tea.Batch(cmd, slashCmd)
	}
	if fileCmd := m.checkFilePopup(msg); fileCmd != nil {
		return m, tea.Batch(cmd, fileCmd)
	}

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
			m.insertSlashCompletion(cmd)
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

// checkSlashPopup opens the slash popup when the user types "/".
func (m *Model) checkSlashPopup(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "/" && !m.slashPopup.Active() && !m.filePopup.Active() {
		spec := parseSlashSpec(m.input.Value())
		if spec.Active {
			m.slashPopup.Open(spec.Query)
			return nil
		}
	}
	// Update filter if slash popup is active.
	if m.slashPopup.Active() {
		spec := parseSlashSpec(m.input.Value())
		if !spec.Active || spec.HasArgs {
			m.slashPopup.Close()
		} else {
			m.slashPopup.UpdateFilter(spec.Query)
		}
	}
	return nil
}

// checkSlashPopupActive updates or closes the slash popup based on current input.
// Used when there's no key event (e.g. paste).
func (m *Model) checkSlashPopupActive() {
	if !m.slashPopup.Active() {
		return
	}
	spec := parseSlashSpec(m.input.Value())
	if !spec.Active || spec.HasArgs {
		m.slashPopup.Close()
	} else {
		m.slashPopup.UpdateFilter(spec.Query)
	}
}

// handleFilePopupKey handles key events when the file (@) popup is active.
func (m *Model) handleFilePopupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "up":
		m.filePopup.SelectUp()
		return m, nil
	case "down":
		m.filePopup.SelectDown()
		return m, nil
	case "esc":
		m.filePopup.Close()
		return m, nil
	case "tab", "enter":
		if path, ok := m.filePopup.SelectedFile(); ok {
			m.insertFileCompletion(path)
			m.filePopup.Close()
		}
		return m, nil
	case "space":
		m.filePopup.Close()
		// Fall through to insert space into textarea.
	}

	// For all other keys, pass to textarea.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Update popup filter or close if no longer matching.
	spec := parseAtSpec(m.input.Value(), m.projectDir)
	if !spec.Active {
		m.filePopup.Close()
	} else {
		if loadCmd := m.filePopup.UpdateFilter(spec); loadCmd != nil {
			return m, tea.Batch(cmd, loadCmd)
		}
	}

	return m, cmd
}

// checkFilePopup opens the file popup when the user types @.
func (m *Model) checkFilePopup(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "@" && !m.filePopup.Active() && !m.slashPopup.Active() {
		spec := parseAtSpec(m.input.Value(), m.projectDir)
		if spec.Active {
			return m.filePopup.Open(spec)
		}
		return nil
	}
	// Update filter if file popup is active
	if m.filePopup.Active() {
		spec := parseAtSpec(m.input.Value(), m.projectDir)
		if !spec.Active {
			m.filePopup.Close()
		} else {
			return m.filePopup.UpdateFilter(spec)
		}
	}
	return nil
}

// insertFileCompletion replaces the @query in the input with the selected file path.
func (m *Model) insertFileCompletion(path string) {
	value := m.input.Value()
	spec := parseAtSpec(value, m.projectDir)
	if !spec.Active {
		return
	}
	// Replace from @ position to end of query with @path
	endIdx := spec.StartIdx + 1 + len(spec.Query)
	if endIdx > len(value) {
		endIdx = len(value)
	}
	newValue := value[:spec.StartIdx] + "@" + path + " " + value[endIdx:]
	m.input.SetValue(newValue)
	m.input.CursorEnd()
}

// insertSlashCompletion replaces the /command in the input with the selected command.
func (m *Model) insertSlashCompletion(cmd string) {
	value := m.input.Value()
	spec := parseSlashSpec(value)
	if !spec.Active {
		return
	}
	// Replace from "/" position to end of command word with the selected command.
	endIdx := spec.StartIdx + len(spec.Command)
	if endIdx > len(value) {
		endIdx = len(value)
	}
	newValue := value[:spec.StartIdx] + cmd + " " + value[endIdx:]
	m.input.SetValue(newValue)
	m.input.CursorEnd()
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
			m.tasks = nil
		}
		return nil
	case "/compact":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.CompactAction{})
			m.status = "Compacting"
		}
		return nil
	case "/truncate-tool-result":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.TruncateToolResultsAction{})
			m.status = "Truncating tool results"
		}
		return nil
	case "/title":
		if m.currentSessionID != "" {
			if actor, ok := m.sender.(Actor); ok {
				actor.Do(protocol.AutoTitleSessionAction{SessionID: m.currentSessionID})
				m.status = "Generating title"
			}
		} else {
			m.status = "No session to title"
		}
		return nil
	case "/skills":
		if m.skillStore != nil {
			m.transcript.appendDone(blockInfo, "skills", skill.FormatSkillList(m.skillStore.All()))
			m.status = "Skills listed"
		}
		return nil
	case "/mcp":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ListMCPAction{})
			m.status = "Loading MCP servers"
		}
		return nil
	case "/tool":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ListToolsAction{})
			m.status = "Loading tools"
		}
		return nil
	case "/dryrun":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.DryRunRequestAction{Input: spec.Args})
			m.status = "Dry run"
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

// dequeueLast pops the last queued message back into the input for editing.
func (m *Model) dequeueLast() {
	if len(m.queued) == 0 {
		return
	}
	last := m.queued[len(m.queued)-1]
	m.queued = m.queued[:len(m.queued)-1]
	if len(m.queued) == 0 {
		m.status = "Ready"
	} else {
		m.status = fmt.Sprintf("Queued (%d)", len(m.queued))
	}
	m.input.SetValue(last)
	m.input.CursorEnd()
	if actor, ok := m.sender.(Actor); ok {
		actor.Do(protocol.DequeueLastInputAction{})
	}
}

func (m *Model) cancelTurn(status string) {
	if actor, ok := m.sender.(Actor); ok {
		actor.Do(protocol.CancelAction{})
	}
	m.busy = false
	m.queued = nil
	m.runningAgents = nil
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

// ── Running Agent tracking ──────────────────────────────────────────────────

type runningAgent struct {
	ID          string
	Description string
	Activity    string
}

func (m *Model) upsertRunningAgent(id, description string) {
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == id {
			m.runningAgents[i].Description = description
			return
		}
	}
	m.runningAgents = append(m.runningAgents, runningAgent{ID: id, Description: description, Activity: "running"})
}

func (m *Model) updateRunningAgentActivity(id, activity string) {
	activity = strings.TrimSpace(activity)
	if activity == "" {
		return
	}
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == id {
			m.runningAgents[i].Activity = activity
			return
		}
	}
}

func (m *Model) removeRunningAgent(id string) {
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == id {
			m.runningAgents = append(m.runningAgents[:i], m.runningAgents[i+1:]...)
			return
		}
	}
}

func (m *Model) agentBarHeight() int {
	return len(m.runningAgents) * 2
}

func (m *Model) agentBarView() string {
	if len(m.runningAgents) == 0 {
		return ""
	}
	var b strings.Builder
	for _, a := range m.runningAgents {
		icon := "■" // solid square, blinks via statusFrame
		if m.statusFrame%4 >= 2 {
			icon = "□" // hollow square for blink effect
		}
		label := m.styles.Agent.Label.Render(fmt.Sprintf("[Agent: %s]", a.Description))
		activity := a.Activity
		if activity == "" {
			activity = "running"
		}
		line := fmt.Sprintf("%s %s", icon, activity)
		b.WriteString(label)
		b.WriteByte('\n')
		b.WriteString(m.styles.Agent.Running.Render(line))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
