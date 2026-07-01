package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/ui/theme"
	"github.com/zhanglvtao/cece/internal/update"
	"github.com/zhanglvtao/cece/internal/version"
)

const (
	simpleInputMinHeight = 1
	simpleInputMaxHeight = 8
	modalMaxHeight       = 14
	horizontalPadding    = 2
	inputHorizontalPad   = 1
	inputShadowHeight    = 1
)

var statusSpinnerFrames = []rune{'-', '\\', '|', '/'}

type globalEventMsg struct{ events []protocol.Event }
type inputErrorMsg struct{ err error }
type statusSpinnerTickMsg struct{}
type updateAvailableMsg struct {
	current string
	latest  string
}
type vimFinishedMsg struct{}
type viewFileMsg struct {
	path    string
	content string
	err     error
}
type shellResultMsg struct {
	command  string
	output   string
	isError  bool
	duration time.Duration
}

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

type layoutState struct {
	headerBar  string
	headerBarH int
	titleBar   string
	titleBarH  int
	modal      string
	modalH     int
	popup      string
	popupH     int
	filePopup  string
	filePopupH int
	taskBar    string
	taskBarH   int
	agentBar   string
	agentBarH  int
	queued     string
	queuedH    int
	headline   string
	headlineH  int
	inputH     int
	statusH    int
	viewportH  int
	separatorH int
}

func renderedHeight(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// Model is Cece's root Bubble Tea model. It intentionally keeps UI state small:
// protocol events update the transcript, and protocol actions drive the runtime.
type Model struct {
	sender Sender

	modelName           string
	currentEffort       string // current reasoning effort level
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
	requestStartTime    time.Time // when the current request started

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
	headerBar  *HeaderBar

	sessions                session.Store
	currentSessionID        string
	currentSessionTitle     string
	currentSessionEphemeral bool
	pendingQuit             bool      // set on ctrl+c, quit after title generation completes
	shouldQuit              bool      // set by applyEvent when pendingQuit title is done
	lastEmptyCtrlC          time.Time // timestamp of last ctrl+c when input was empty
	skillStore              *skill.Store
	queued                  []string
	history                 []string
	historyIndex            int
	viewportDirty           bool // true when transcript content changed, cleared after refresh
	viewportGotoBottom      bool // when dirty, whether to pin viewport to bottom
	observatoryURL          string
	lastObservatoryPost     time.Time
	lastObservatorySig      string
	lastViewportWidth       int  // track width changes for refresh
	scrollToPlanBlock       bool // scroll viewport to plan block's first line after PlanApprovalRequested
	viewMode                bool // true when file popup is in /view mode (Enter reads file, not inserts path)
	appliedEventCount       int
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
	defaultEffort := "xhigh"

	sb := NewStatusBar()
	sb.UpdateMode(string(protocol.PermissionModeDefault))
	sb.UpdateModel(modelName)
	sb.UpdateEffort(defaultEffort)
	sb.UpdateContext(0, cw)

	hb := NewHeaderBar()

	return Model{
		sender:        sender,
		modelName:     modelName,
		currentEffort: defaultEffort,
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
		headerBar:     hb,
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

// SetDefaultEffort sets the initial reasoning effort from config.
func (m *Model) SetDefaultEffort(effort string) {
	if effort == "" {
		return
	}
	m.currentEffort = effort
	if m.statusBar != nil {
		m.statusBar.UpdateEffort(effort)
	}
}

func (m *Model) SetSkillStore(store *skill.Store) {
	m.skillStore = store
	if store != nil {
		m.slashPopup.SetSkills(store.Enabled())
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{func() tea.Msg { return tea.RequestBackgroundColor() }}
	if eventer, ok := m.sender.(Eventer); ok {
		cmds = append(cmds, consumeGlobalEventsCmd(eventer.Events()))
	}
	cmds = append(cmds, checkUpdateCmd())
	return tea.Batch(cmds...)
}

func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		// Avoid blocking startup; use a short timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		info, err := update.Check(ctx, version.Version)
		if err != nil || !info.Available() {
			return nil
		}
		return updateAvailableMsg{current: info.Current, latest: info.Latest}
	}
}

func consumeGlobalEventsCmd(ch <-chan protocol.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		events := []protocol.Event{ev}
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					return globalEventMsg{events: events}
				}
				events = append(events, e)
			default:
				if len(events) >= 8 {
					timer := time.NewTimer(8 * time.Millisecond)
					defer timer.Stop()
				batchMore:
					for {
						select {
						case e, ok := <-ch:
							if !ok {
								return globalEventMsg{events: events}
							}
							events = append(events, e)
						case <-timer.C:
							break batchMore
						}
					}
				}
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cwBefore := m.contextWindow
	result, cmd := m.update(msg)
	if resultModel, ok := result.(*Model); ok {
		if resultModel.contextWindow != cwBefore {
			logger.Info("Update: contextWindow changed", "old", cwBefore, "new", resultModel.contextWindow, "msgType", fmt.Sprintf("%T", msg))
		}
	}
	return result, cmd
}

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
		m.transcript.invalidateAllCaches()
		m.viewportDirty = true
		return m, nil
	case inputErrorMsg:
		m.busy = false
		errMsg := appendErrorContext(msg.err.Error())
		m.status = msg.err.Error()
		m.transcript.appendDone(blockError, "error", errMsg)
		m.viewportDirty = true
		return m, nil
	case statusSpinnerTickMsg:
		if !m.statusShowsSpinner() && !(m.busy && m.hasInProgressTask()) && len(m.runningAgents) == 0 {
			m.statusSpinnerActive = false
			return m, nil
		}
		m.statusFrame++
		return m, statusSpinnerTickCmd()
	case filesLoadedMsg:
		m.filePopup.OnFilesLoaded(msg.root)
		return m, nil
	case globalEventMsg:
		for _, ev := range msg.events {
			cwBefore := m.contextWindow
			m.applyEvent(ev)
			m.appliedEventCount++
			if m.contextWindow != cwBefore {
				logger.Info("UI: contextWindow changed during applyEvent", "old", cwBefore, "new", m.contextWindow, "eventType", fmt.Sprintf("%T", ev))
			}
		}
		m.viewportDirty = true
		if m.shouldQuit {
			m.shouldQuit = false
			return m, func() tea.Msg { return tea.Quit() }
		}
		cmds := []tea.Cmd{}
		if cmd := m.ensureStatusSpinner(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.maybePostObservatorySnapshot(); cmd != nil {
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
		if m.modal.active() && m.modal.textMode {
			m.modal.textInput += msg.Content
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.checkSlashPopupActive()
		m.filePopup.Close()
		return m, cmd
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case updateAvailableMsg:
		m.status = fmt.Sprintf("Update: v%s → v%s (cece update)", msg.current, msg.latest)
		return m, nil
	case vimFinishedMsg:
		m.viewportDirty = true
		return m, nil
	case viewFileMsg:
		if msg.err != nil {
			m.transcript.appendDone(blockError, "view error", msg.err.Error())
			m.status = "View error"
		} else {
			lang := langFromPath(msg.path)
			title := "view: " + filepath.Base(msg.path)
			idx := m.transcript.append(blockView, title, msg.content)
			m.transcript.blocks[idx].toolParams = lang
			m.transcript.blocks[idx].done = true
			m.status = "View: " + filepath.Base(msg.path)
		}
		m.viewportDirty = true
		m.viewportGotoBottom = true
		return m, nil
	case shellResultMsg:
		result := tailLines(msg.output, 20)
		result = strings.TrimRight(result, "\n")
		status := formatBashStatus(msg.isError, msg.duration)
		idx := m.transcript.append(blockTool, "shell: "+msg.command, result+"\n"+status)
		m.transcript.blocks[idx].toolName = "Bash" // reuse Bash rendering (no indent)
		m.transcript.blocks[idx].done = true
		if msg.isError {
			m.transcript.blocks[idx].err = true
		}
		m.viewportDirty = true
		m.viewportGotoBottom = true
		m.status = "Shell completed"
		// Append to conversation history (stripped of ANSI codes).
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.AppendShellResultAction{
				Command: msg.command,
				Output:  msg.output,
				IsError: msg.isError,
			})
		}
		return m, nil
	}

	if !m.modal.active() {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ApplyEventForTest applies a protocol event directly to the model. For testing only.
// It calls resize() after applyEvent so that viewport state is immediately
// consistent (production code defers viewport refresh to View()).
func (m *Model) ApplyEventForTest(event protocol.Event) {
	m.applyEvent(event)
	m.appliedEventCount++
	m.resize()
}

func (m *Model) applyEvent(event protocol.Event) {
	m.transcript.apply(event)
	switch e := event.(type) {
	case protocol.ObservatoryServerStartedEvent:
		m.observatoryURL = e.URL
		return
	case protocol.EngineReadyEvent:
		if e.ContextWindow > 0 && e.ContextWindow != m.contextWindow {
			logger.Info("UI: contextWindow synced from EngineReadyEvent", "old", m.contextWindow, "new", e.ContextWindow)
			m.contextWindow = e.ContextWindow
		}
		if e.Model != "" {
			m.modelName = e.Model
			m.statusBar.UpdateModel(e.Model)
		}
		if e.Effort != "" {
			m.currentEffort = e.Effort
			m.statusBar.UpdateEffort(e.Effort)
		}
	case protocol.SessionCreated:
		m.currentSessionID = e.ID
		m.currentSessionTitle = ""
		m.currentSessionEphemeral = true
		m.status = "Session created"
	case protocol.SessionTitleGeneratedEvent:
		if e.Err != "" {
			m.status = errorStatus("Title generation failed")
		} else {
			m.status = "Title: " + e.Title
			if e.SessionID == m.currentSessionID {
				m.currentSessionTitle = e.Title
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
		m.requestStartTime = time.Now()
		if e.ContextWindow > 0 && e.ContextWindow != m.contextWindow {
			logger.Info("UI: contextWindow synced from ModelRequestStarted", "old", m.contextWindow, "new", e.ContextWindow)
			m.contextWindow = e.ContextWindow
		}
	case protocol.AssistantStarted:
		m.busy = true
		m.status = "Streaming"
		m.streamHeadline = ""
	case protocol.AssistantDelta:
		m.streamHeadline += e.Text
	case protocol.RunFailed:
		m.busy = false
		m.queued = nil
		m.requestStartTime = time.Time{}
		m.status = errorStatus("Failed")
		m.streamHeadline = ""
		m.headerBar.IncrementTurn(false)
		m.headerBar.IncrementAPI(false)
	case protocol.TurnCompleted:
		m.busy = false
		m.requestStartTime = time.Time{}
		m.status = "Ready"
		m.streamHeadline = ""
		m.headerBar.IncrementTurn(true)
		if e.ContextWindow > 0 && e.ContextWindow != m.contextWindow {
			logger.Info("UI: contextWindow synced from TurnCompleted", "old", m.contextWindow, "new", e.ContextWindow)
			m.contextWindow = e.ContextWindow
		}
	case protocol.QueuedInputPromoted:
		if len(m.queued) > 0 {
			m.queued = m.queued[1:]
		}
	case protocol.TruncationRetry:
		m.status = "Retrying"
	case protocol.ToolCallCompleted:
		// tool count is set from ToolExecCompleted
	case protocol.ToolExecCompleted:
		m.headerBar.IncrementTool(e.Name, !e.Result.IsError)
	case protocol.StreamCompleted:
		logger.Info("model stream completed",
			"input_tokens", e.InputTokens,
			"output_tokens", e.OutputTokens,
			"stop_reason", e.StopReason,
			"duration", e.Duration,
			"tool_calls", e.ToolCalls,
		)
		m.headerBar.IncrementAPI(true)
	case protocol.ToolCallsReady:
		m.openToolConfirm(e.Calls)
		m.status = "Confirm tools"
	case protocol.PlanApprovalRequested:
		m.openPlanConfirm(e.PlanFile)
		m.status = "Approve plan"
		m.scrollToPlanBlock = true
	case protocol.PlanRejected:
		m.mode = protocol.PermissionModePlan
		m.status = "Plan rejected"
	case protocol.ToolCallsRejected:
		m.status = "Tool calls rejected"
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
	case protocol.EffortChangedEvent:
		m.currentEffort = e.Effort
		m.statusBar.UpdateEffort(e.Effort)
	case protocol.ModeEvent:
		m.mode = e.Mode
	case protocol.SessionLoadedEvent:
		if e.Err != "" {
			m.status = errorStatus("Failed to load session: " + e.Err)
		} else {
			m.currentSessionID = e.SessionID
			m.currentSessionEphemeral = false
			m.currentSessionTitle = ""
			if m.sessions != nil {
				if s, err := m.sessions.Get(context.Background(), e.SessionID); err == nil && s.Title != "" {
					m.currentSessionTitle = s.Title
				}
			}
			if e.Model != "" {
				m.modelName = e.Model
				m.statusBar.UpdateModel(e.Model)
			}
			if e.ContextWindow > 0 {
				logger.Info("UI: contextWindow changed by SessionLoadedEvent", "old", m.contextWindow, "new", e.ContextWindow)
				m.contextWindow = e.ContextWindow
			}
			m.headerBar.Restore(e.APICalls, e.ToolCounts, e.CacheReadTokens, e.TurnCount)
			if len(e.InputHistory) > 0 {
				m.history = e.InputHistory
				m.historyIndex = -1
			}
			m.status = "Session loaded"
		}
	case protocol.HistoryClearedEvent:
		m.transcript.reset()
		m.status = "Cleared"
	case protocol.CompactingEvent:
		m.status = "Compacting"
	case protocol.CompactedEvent:
		if e.Err != "" {
			m.status = errorStatus("Compact failed: " + e.Err)
			m.transcript.appendDone(blockError, "compact", "Compact failed: "+e.Err)
		} else if e.MessagesBefore == e.MessagesAfter {
			m.status = "Not enough messages to compact"
			m.transcript.appendDone(blockInfo, "compact", "Not enough messages to compact. Send a few more messages first.")
		} else {
			m.status = fmt.Sprintf("Compacted %d→%d msgs, %dK→%dK tokens",
				e.MessagesBefore, e.MessagesAfter,
				(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
			m.transcript.appendDone(blockInfo, "compact", e.Summary)
			m.transcript.contextUsed = e.TokensAfter
		}
	case protocol.TruncatedToolResultsEvent:
		m.status = fmt.Sprintf("Truncated %d tool results, %dK→%dK tokens",
			e.TruncatedCount,
			(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
		m.transcript.contextUsed = e.TokensAfter
	case protocol.PrunedEvent:
		m.status = fmt.Sprintf("Pruned %d turns, %dK→%dK tokens",
			e.PrunedTurns,
			(e.TokensBefore+999)/1000, (e.TokensAfter+999)/1000)
		m.transcript.contextUsed = e.TokensAfter
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
		m.updateRunningAgentActivity(e)
	case protocol.SubAgentCompletedEvent:
		m.markAgentDone(e.ID, "completed")
	case protocol.SubAgentFailedEvent:
		m.markAgentDone(e.ID, "failed")
		m.status = errorStatus(fmt.Sprintf("● %s failed: %s", e.Description, e.Error))
	}
	// Sync status bar and header bar data from model state.
	m.statusBar.UpdateMode(string(m.mode))
	m.statusBar.UpdateContext(m.transcript.contextUsed, m.contextWindow)
	m.headerBar.UpdateTokens(m.transcript.inputTokens, m.transcript.outputTokens, m.transcript.cacheReadTokens)
	m.viewportDirty = true
	if eventPinsViewportToBottom(event) {
		m.viewportGotoBottom = true
	}
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
	contentWidth := m.contentWidth()
	sep := m.styles.Status.Separator.Render(strings.Repeat("─", max(contentWidth, 0)))
	ls := m.measureLayout()
	sections := []string{}
	// Header bar at very top: stats
	if ls.headerBar != "" {
		sections = append(sections, ls.headerBar, sep)
	}
	// Title bar at top: session title + id
	if ls.titleBar != "" {
		sections = append(sections, ls.titleBar, sep)
	}
	sections = append(sections, m.viewport.View())
	if ls.modal != "" {
		sections = append(sections, sep)
		sections = append(sections, ls.modal)
	}
	// Task bar: bordered block with label
	if ls.taskBar != "" {
		sections = append(sections, sep)
		sections = append(sections, ls.taskBar)
		sections = append(sections, sep)
	} else if ls.agentBar != "" || ls.headline != "" || ls.queued != "" {
		sections = append(sections, sep)
	}
	if ls.agentBar != "" {
		sections = append(sections, ls.agentBar)
		sections = append(sections, sep)
	}
	if ls.queued != "" {
		sections = append(sections, ls.queued)
		sections = append(sections, sep)
	}
	// headline (e.g. "Requesting") is always directly above input, no separator
	if ls.headline != "" {
		sections = append(sections, ls.headline)
	}
	// Popups must be directly above input box
	if ls.popup != "" {
		sections = append(sections, ls.popup)
	}
	if ls.filePopup != "" {
		sections = append(sections, ls.filePopup)
	}
	sections = append(sections, m.inputView())
	statusBarView := m.statusBar.Render(contentWidth)
	if statusBarView != "" {
		sections = append(sections, statusBarView)
	}
	content := strings.Join(sections, "\n")
	if pad := m.horizontalPadding(); pad > 0 {
		content = lipgloss.NewStyle().Padding(0, pad).Render(content)
	}
	view := tea.NewView(content)
	view.MouseMode = tea.MouseModeCellMotion
	view.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true

	// Position cursor.
	if m.modal.kind == modalQuestion && m.modal.textMode {
		// Place cursor at the inline text input line inside the question modal.
		cur := &tea.Cursor{}
		cur.Y = m.viewport.Height() + ls.modalH - 1
		if ls.headerBarH > 0 {
			cur.Y += ls.headerBarH + 1
		}
		if ls.titleBarH > 0 {
			cur.Y += ls.titleBarH + 1
		}
		cur.X = 6 + uniseg.StringWidth(m.modal.textInput) // "> [ ] " prefix (6 chars) + typed text display width
		view.Cursor = cur
	} else if cur := m.input.Cursor(); cur != nil {
		rowsAboveInput := m.viewport.Height()
		if ls.headerBarH > 0 {
			rowsAboveInput += ls.headerBarH + 1
		}
		if ls.titleBarH > 0 {
			rowsAboveInput += ls.titleBarH + 1
		}
		if ls.modalH > 0 {
			rowsAboveInput += 1 + ls.modalH
		}
		if ls.taskBarH > 0 {
			rowsAboveInput += 1 + ls.taskBarH + 1
		} else if ls.agentBarH > 0 || ls.headlineH > 0 || ls.queuedH > 0 {
			rowsAboveInput++
		}
		if ls.agentBarH > 0 {
			rowsAboveInput += ls.agentBarH + 1
		}
		if ls.queuedH > 0 {
			rowsAboveInput += ls.queuedH + 1
		}
		if ls.headlineH > 0 {
			rowsAboveInput += ls.headlineH
		}
		rowsAboveInput += ls.popupH + ls.filePopupH
		metrics := inputMetrics(contentWidth, m.input.Height())
		cur.Y += rowsAboveInput + metrics.CursorYPad
		cur.X += metrics.CursorXPad + m.horizontalPadding()
		view.Cursor = cur
	}

	return view
}

func (m *Model) measureLayout() layoutState {
	ls := layoutState{separatorH: 1}
	ls.headerBar = m.headerBarView()
	ls.headerBarH = renderedHeight(ls.headerBar)
	ls.titleBar = m.titleBarView()
	ls.titleBarH = renderedHeight(ls.titleBar)
	ls.modal = m.modalView()
	ls.modalH = renderedHeight(ls.modal)
	contentWidth := m.contentWidth()
	ls.popup = m.slashPopup.View(contentWidth)
	ls.popupH = renderedHeight(ls.popup)
	ls.filePopup = m.filePopup.View(contentWidth)
	ls.filePopupH = renderedHeight(ls.filePopup)
	ls.taskBar = m.taskBarView()
	ls.taskBarH = renderedHeight(ls.taskBar)
	ls.agentBar = m.agentBarView()
	ls.agentBarH = renderedHeight(ls.agentBar)
	ls.queued = m.queuedListView()
	ls.queuedH = renderedHeight(ls.queued)
	ls.headline = m.headlineView()
	ls.headlineH = renderedHeight(ls.headline)
	ls.inputH = inputMetrics(contentWidth, m.input.Height()).TotalH
	ls.statusH = m.statusBar.Height()

	chromeH := ls.inputH + ls.statusH
	if ls.headerBarH > 0 {
		chromeH += ls.headerBarH + ls.separatorH
	}
	if ls.titleBarH > 0 {
		chromeH += ls.titleBarH + ls.separatorH
	}
	if ls.modalH > 0 {
		chromeH += ls.separatorH + ls.modalH
	}
	if ls.taskBarH > 0 {
		chromeH += ls.separatorH + ls.taskBarH + ls.separatorH
	} else if ls.agentBarH > 0 || ls.headlineH > 0 || ls.queuedH > 0 {
		chromeH += ls.separatorH
	}
	if ls.agentBarH > 0 {
		chromeH += ls.agentBarH + ls.separatorH
	}
	if ls.queuedH > 0 {
		chromeH += ls.queuedH + ls.separatorH
	}
	if ls.headlineH > 0 {
		chromeH += ls.headlineH
	}
	chromeH += ls.popupH + ls.filePopupH

	ls.viewportH = m.height - chromeH
	if ls.viewportH < 3 {
		ls.viewportH = 3
	}
	return ls
}

func (m *Model) resize() {
	atBottom := m.viewport.AtBottom()
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}
	contentWidth := m.contentWidth()
	if !m.viewport.AtBottom() {
		m.statusBar.UpdateScroll(int(m.viewport.ScrollPercent() * 100))
	} else {
		m.statusBar.UpdateScroll(0)
	}
	ls := m.measureLayout()
	viewportH := ls.viewportH
	metrics := inputMetrics(contentWidth, m.input.Height())
	m.viewport.SetWidth(contentWidth)
	m.viewport.SetHeight(viewportH)
	m.input.SetWidth(metrics.TextareaW)
	m.input.SetHeight(metrics.TextareaH)
	widthChanged := m.lastViewportWidth != contentWidth
	if widthChanged || m.viewportDirty {
		m.refreshViewport(atBottom || m.viewportGotoBottom)
		m.viewportDirty = false
		m.viewportGotoBottom = false
		m.lastViewportWidth = contentWidth
	}
}

func (m *Model) refreshViewport(gotoBottom bool) {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.transcript.render(m.contentWidth(), m.styles))
	if m.scrollToPlanBlock {
		m.scrollToPlanBlock = false
		if offset, found := m.transcript.lastPlanOffset(m.contentWidth(), m.styles); found {
			m.viewport.SetYOffset(offset)
			return
		}
	}
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
		b.WriteString("○ ")
		b.WriteString(msg)
	}
	return b.String()
}

func (m *Model) horizontalPadding() int {
	if m.width <= 0 {
		return 0
	}
	pad := horizontalPadding
	if m.width < pad*2+20 {
		pad = max(0, (m.width-20)/2)
	}
	return pad
}

func (m *Model) contentWidth() int {
	width := m.width - m.horizontalPadding()*2
	if width < 20 {
		return 20
	}
	return width
}

type inputSurfaceMetrics struct {
	ContentWidth int
	TextareaW    int
	TextareaH    int
	TotalH       int
	CursorXPad   int
	CursorYPad   int
}

func inputMetrics(contentWidth, textareaHeight int) inputSurfaceMetrics {
	if contentWidth < 1 {
		contentWidth = 1
	}
	textareaW := contentWidth - inputHorizontalPad*2
	if textareaW < 1 {
		textareaW = 1
	}
	textareaH := clamp(textareaHeight, simpleInputMinHeight, simpleInputMaxHeight)
	return inputSurfaceMetrics{
		ContentWidth: contentWidth,
		TextareaW:    textareaW,
		TextareaH:    textareaH,
		TotalH:       textareaH + inputShadowHeight,
		CursorXPad:   inputHorizontalPad,
		CursorYPad:   0,
	}
}

func padInputView(input string, width int) string {
	lines := strings.Split(input, "\n")
	pad := strings.Repeat(" ", inputHorizontalPad)
	for i, line := range lines {
		line = pad + line
		visible := lipgloss.Width(line)
		if visible < width {
			line += strings.Repeat(" ", width-visible)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func inputShadowLine(width int, busy bool, shell bool) string {
	if width < 1 {
		width = 1
	}
	color := theme.FgMuted
	if busy {
		color = theme.Primary
	}
	if shell {
		color = theme.Yellow
	}
	return lipgloss.NewStyle().Background(color).Render(strings.Repeat(" ", width))
}

func (m *Model) inputView() string {
	metrics := inputMetrics(m.contentWidth(), m.input.Height())
	shell := strings.HasPrefix(strings.TrimSpace(m.input.Value()), "!")
	body := padInputView(m.input.View(), metrics.ContentWidth)
	shadow := inputShadowLine(metrics.ContentWidth, m.busy, shell)
	return body + "\n" + shadow
}

// formatDuration formats a duration as whole seconds: "38s", "1m2s", etc.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

func sweepWindowStart(textLen, window, frame int) int {
	if textLen <= 0 || window >= textLen {
		return 0
	}
	maxStart := textLen - window
	cycle := maxStart * 2
	if cycle == 0 {
		return 0
	}
	step := frame % cycle
	if step <= maxStart {
		return step
	}
	return cycle - step
}

func renderSweepingText(text string, frame int, base lipgloss.Style) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	window := 4
	if len(runes) < window {
		window = len(runes)
	}
	start := sweepWindowStart(len(runes), window, frame)
	highlight := base.Foreground(theme.Fg).Background(theme.Primary).Bold(true)

	var b strings.Builder
	for i, r := range runes {
		style := base
		if i >= start && i < start+window {
			style = highlight
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

// headlineView renders a one-line indicator above the input surface.
// Shows "<spinner> <status>" when idle (e.g. "- Ready"),
// and "<spinner> <status> (<elapsed>) | <streamHeadline>" when busy streaming.
// Active request statuses use ANSI styling for a left-right sweep highlight.
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
	// Colorize the status portion. During active request statuses, add a
	// left-right sweep highlight driven by the existing statusFrame ticker.
	prefix := m.styles.Headline.Render(statusText)
	if m.busy || m.statusShowsSpinner() {
		prefix = renderSweepingText(statusText, m.statusFrame, m.styles.Headline)
	}
	// Append elapsed time if a request is in progress
	if m.busy && !m.requestStartTime.IsZero() {
		elapsed := time.Since(m.requestStartTime)
		prefix += " (" + formatDuration(elapsed) + ")"
	}
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
	maxLen := m.contentWidth()
	if maxLen < 10 {
		maxLen = 10
	}
	return ansi.Truncate(prefix, maxLen, "...")
}

func (m *Model) titleBarView() string {
	parts := []string{}
	contentWidth := m.contentWidth()
	if m.currentSessionTitle != "" {
		parts = append(parts, m.currentSessionTitle)
	}
	if m.currentSessionID != "" {
		parts = append(parts, "session-id:"+m.currentSessionID)
	}
	if m.observatoryURL != "" {
		parts = append(parts, "obs:"+m.observatoryURL)
	}
	if len(parts) == 0 {
		return ""
	}
	return ansi.Truncate(m.styles.TitleBar.Render(strings.Join(parts, " · ")), contentWidth, "")
}

func (m *Model) headerBarView() string {
	return m.headerBar.Render(m.contentWidth())
}

func (m *Model) statusShowsSpinner() bool {
	return strings.HasSuffix(m.status, "ing")
}

func (m *Model) ensureStatusSpinner() tea.Cmd {
	if !m.statusShowsSpinner() && !(m.busy && m.hasInProgressTask()) && len(m.runningAgents) == 0 {
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
	// Fast path: if the key contains non-ASCII text (e.g. CJK characters from
	// IME), send it directly to the textarea. This prevents any shortcut or
	// popup handler from intercepting IME-committed text.
	// Exception: when modal is active and in textMode, route to modal instead.
	if text := msg.Key().Text; text != "" && !isASCII(text) {
		if m.modal.active() && m.modal.textMode {
			m.modal.textInput += text
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.checkSlashPopupActive()
		m.filePopup.Close()
		return m, cmd
	}

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
			m.lastEmptyCtrlC = time.Time{} // reset double-ctrl+c timer
			return m, nil
		}
		// Input is empty — check for double ctrl+c to force quit without title.
		now := time.Now()
		if !m.lastEmptyCtrlC.IsZero() && now.Sub(m.lastEmptyCtrlC) < time.Second {
			// Double ctrl+c: delete session and quit immediately.
			if m.currentSessionID != "" {
				if actor, ok := m.sender.(Actor); ok {
					actor.Do(protocol.DeleteSessionAction{SessionID: m.currentSessionID})
				}
			}
			return m, func() tea.Msg { return tea.Quit() }
		}
		m.lastEmptyCtrlC = now
		// Single ctrl+c with empty input — request auto-title then quit.
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
	if viewCmd := m.checkViewFilePopup(msg); viewCmd != nil {
		return m, tea.Batch(cmd, viewCmd)
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
			if cmd == "/view" {
				m.viewMode = true
				m.insertSlashCompletion(cmd)
				m.slashPopup.Close()
				// Open file popup for project root with empty filter.
				vspec := parseViewSpec(m.input.Value(), m.projectDir)
				if !vspec.Active {
					// No args yet — open with empty spec to browse all files.
					vspec = atSpec{Active: true, AbsRoot: m.projectDir, FileName: ""}
				}
				return m, m.filePopup.Open(vspec)
			}
			m.insertSlashCompletion(cmd)
			m.slashPopup.Close()
		}
		return m, nil
	case "space":
		// If committed command is /view, open file popup.
		spec := parseSlashSpec(m.input.Value())
		if spec.Active && spec.Command == "/view" {
			m.viewMode = true
			m.slashPopup.Close()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			vspec := parseViewSpec(m.input.Value(), m.projectDir)
			if !vspec.Active {
				vspec = atSpec{Active: true, AbsRoot: m.projectDir, FileName: ""}
			}
			return m, tea.Batch(cmd, m.filePopup.Open(vspec))
		}
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
		m.viewMode = false
		return m, nil
	case "tab", "enter":
		if path, ok := m.filePopup.SelectedFile(); ok {
			if m.viewMode {
				m.filePopup.Close()
				m.viewMode = false
				m.input.Reset()
				absPath := path
				if !filepath.IsAbs(absPath) {
					absPath = filepath.Join(m.projectDir, path)
				}
				return m, viewFileCmd(absPath)
			}
			m.insertFileCompletion(path)
			m.filePopup.Close()
		}
		return m, nil
	case "ctrl+o":
		if path, ok := m.filePopup.SelectedFile(); ok {
			m.filePopup.Close()
			m.viewMode = false
			absPath := path
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(m.projectDir, path)
			}
			return m, tea.ExecProcess(exec.Command("vim", absPath), func(err error) tea.Msg {
				return vimFinishedMsg{}
			})
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
	if m.viewMode {
		spec := parseSlashSpec(m.input.Value())
		if !spec.Active || spec.Command != "/view" {
			m.filePopup.Close()
			m.viewMode = false
			return m, cmd
		}
		vspec := parseViewSpec(m.input.Value(), m.projectDir)
		if !vspec.Active {
			vspec = atSpec{Active: true, AbsRoot: m.projectDir, FileName: ""}
		}
		if loadCmd := m.filePopup.UpdateFilter(vspec); loadCmd != nil {
			return m, tea.Batch(cmd, loadCmd)
		}
		return m, cmd
	}
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

// checkViewFilePopup opens the file popup when the user types /view followed by a space.
func (m *Model) checkViewFilePopup(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "space" && !m.filePopup.Active() && !m.slashPopup.Active() {
		spec := parseSlashSpec(m.input.Value())
		if spec.Active && spec.Command == "/view" && !m.viewMode {
			m.viewMode = true
			vspec := parseViewSpec(m.input.Value(), m.projectDir)
			if !vspec.Active {
				vspec = atSpec{Active: true, AbsRoot: m.projectDir, FileName: ""}
			}
			return m.filePopup.Open(vspec)
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
	// Shell mode: !command — execute directly without LLM
	if strings.HasPrefix(input, "!") {
		cmd := strings.TrimPrefix(input, "!")
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			m.status = "Empty shell command"
			return nil
		}
		return m.runShellCommand(cmd)
	}
	spec := parseSlashSpec(input)
	if spec.Valid() && spec.StartIdx == 0 {
		return m.handleSlashCommand(input)
	}
	if m.busy {
		if m.status == "Question suspended" {
			if actor, ok := m.sender.(Actor); ok {
				actor.Do(protocol.ResumeQuestionAction{Text: input})
			}
			m.status = "Resuming question"
			return nil
		}
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
	case "/exit":
		if m.busy {
			m.cancelTurn("Exiting")
		}
		return func() tea.Msg { return tea.Quit() }
	case "/model":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.ListModelsAction{})
			m.status = "Loading models"
		}
		return nil
	case "/effort":
		m.openEffortPicker()
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
		m.openSkillPicker()
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
	case "/view":
		path := strings.TrimSpace(spec.Args)
		if path == "" {
			m.viewMode = true
			m.input.SetValue("/view ")
			m.input.CursorEnd()
			vspec := atSpec{Active: true, AbsRoot: m.projectDir, FileName: ""}
			return m.filePopup.Open(vspec)
		}
		// Direct path: resolve and read the file
		absPath := path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(m.projectDir, path)
		}
		return viewFileCmd(absPath)
	case "/dryrun":
		if actor, ok := m.sender.(Actor); ok {
			actor.Do(protocol.DryRunRequestAction{Input: spec.Args})
			m.status = "Dry run"
		}
		return nil
	case "/plan":
		return m.openLatestPlan()
	}
	name := strings.TrimPrefix(spec.Command, "/")
	if m.skillStore != nil {
		if sk, ok := m.skillStore.Get(name); ok && m.skillStore.IsEnabled(name) {
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
	// Don't overwrite status — "Requesting"/"Streaming" must remain visible.
	// The queued count is shown in the queued list view above the input.
}

// dequeueLast pops the last queued message back into the input for editing.
func (m *Model) dequeueLast() {
	if len(m.queued) == 0 {
		return
	}
	last := m.queued[len(m.queued)-1]
	m.queued = m.queued[:len(m.queued)-1]
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
		if len(m.history) > 100 {
			m.history = m.history[:100]
		}
	}
	m.historyIndex = -1

	// Persist input history to session store.
	if m.sessions != nil && m.currentSessionID != "" {
		// Copy so the closure captures a stable snapshot.
		histCopy := make([]string, len(m.history))
		copy(histCopy, m.history)
		go func() {
			if err := m.sessions.SaveInputHistory(context.Background(), m.currentSessionID, histCopy); err != nil {
				logger.Error("failed to persist input history", "error", err)
			}
		}()
	}
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

// isASCII reports whether s contains only ASCII characters.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
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

// runShellCommand executes a shell command directly (without LLM) and returns
// a shellResultMsg for display in the transcript.
func (m *Model) runShellCommand(cmd string) tea.Cmd {
	m.status = "Running shell"
	start := time.Now()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "bash", "-c", cmd)
		c.Dir = m.projectDir
		out, err := c.CombinedOutput()
		duration := time.Since(start)
		return shellResultMsg{
			command:  cmd,
			output:   string(out),
			isError:  err != nil,
			duration: duration,
		}
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

// langFromPath returns the language name for a file path, used for syntax highlighting.
func langFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return "markdown"
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".sh":
		return "bash"
	case ".sql":
		return "sql"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	default:
		return ""
	}
}

// viewFileCmd reads a file and returns a viewFileMsg.
func viewFileCmd(absPath string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return viewFileMsg{path: absPath, err: err}
		}
		return viewFileMsg{path: absPath, content: string(data)}
	}
}

// openLatestPlan finds the most recently modified .md file in .cece/plans/
// and displays it using viewFileCmd.
func (m *Model) openLatestPlan() tea.Cmd {
	planDir := filepath.Join(m.projectDir, ".cece", "plans")
	dirs, err := os.ReadDir(planDir)
	if err != nil {
		m.status = "No plans directory found"
		return nil
	}
	type planFile struct {
		path    string
		modTime time.Time
	}
	var plans []planFile
	for _, d := range dirs {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		plans = append(plans, planFile{
			path:    filepath.Join(planDir, d.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(plans) == 0 {
		m.status = "No plan files found"
		return nil
	}
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].modTime.After(plans[j].modTime)
	})
	m.status = "Viewing latest plan"
	return viewFileCmd(plans[0].path)
}

// parseViewSpec extracts the file query from a "/view <path>" input.
func parseViewSpec(input, projectDir string) atSpec {
	spec := parseSlashSpec(input)
	if !spec.Active || spec.Command != "/view" || spec.Args == "" {
		return atSpec{}
	}

	query := spec.Args
	baseDir, fileName := "", query
	if slashIdx := strings.LastIndex(query, "/"); slashIdx >= 0 {
		baseDir = query[:slashIdx+1]
		fileName = query[slashIdx+1:]
	}

	isAbs := strings.HasPrefix(query, "~/") || strings.HasPrefix(query, "/")
	absRoot := projectDir
	if isAbs {
		expanded := expandHome(baseDir)
		if expanded != "" {
			absRoot = expanded
		}
	} else if baseDir != "" {
		absRoot = filepath.Join(projectDir, baseDir)
	}

	return atSpec{
		Active:   true,
		Query:    query,
		BaseDir:  baseDir,
		FileName: fileName,
		AbsRoot:  absRoot,
		IsAbs:    isAbs,
	}
}

// ── Running Agent tracking ──────────────────────────────────────────────────

type runningAgent struct {
	ID              string
	Description     string
	Model           string
	Status          string // "running", "completed", "failed"
	SessionID       string
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	TurnCount       int
	ToolCall        string
	LastMsg         string
	DoneAt          time.Time // when agent completed/failed; zero means still running
}

func (m *Model) upsertRunningAgent(id, description string) {
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == id {
			m.runningAgents[i].Description = description
			return
		}
	}
	m.runningAgents = append(m.runningAgents, runningAgent{ID: id, Description: description})
}

func (m *Model) updateRunningAgentActivity(e protocol.SubAgentActivityEvent) {
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == e.ID {
			a := &m.runningAgents[i]
			if e.Model != "" {
				a.Model = e.Model
			}
			if e.InputTokens > 0 {
				a.InputTokens = e.InputTokens
			}
			if e.OutputTokens > 0 {
				a.OutputTokens = e.OutputTokens
			}
			if e.CacheReadTokens > 0 {
				a.CacheReadTokens = e.CacheReadTokens
			}
			if e.TurnCount > 0 {
				a.TurnCount = e.TurnCount
			}
			if e.ToolCall != "" {
				a.ToolCall = e.ToolCall
			}
			if e.LastAssistantMsg != "" {
				a.LastMsg = e.LastAssistantMsg
			}
			return
		}
	}
}

func (m *Model) markAgentDone(id, status string) {
	for i := range m.runningAgents {
		if m.runningAgents[i].ID == id {
			m.runningAgents[i].Status = status
			m.runningAgents[i].DoneAt = time.Now()
			m.runningAgents[i].ToolCall = ""
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

// agentDoneTTL is how long a completed/failed agent bar entry remains visible.
const agentDoneTTL = 3 * time.Second

func (m *Model) agentBarHeight() int {
	m.purgeDoneAgents()
	if len(m.runningAgents) == 0 {
		return 0
	}
	// Each agent: 1 line for header + 1 line for streaming text.
	// Between agents: 1 blank line.
	n := len(m.runningAgents)
	return n*2 + (n-1)*1
}

// purgeDoneAgents removes agents that have been in done state longer than agentDoneTTL.
func (m *Model) purgeDoneAgents() {
	now := time.Now()
	j := 0
	for _, a := range m.runningAgents {
		if !a.DoneAt.IsZero() && now.Sub(a.DoneAt) > agentDoneTTL {
			continue
		}
		m.runningAgents[j] = a
		j++
	}
	m.runningAgents = m.runningAgents[:j]
}

func (m *Model) agentBarView() string {
	m.purgeDoneAgents()
	if len(m.runningAgents) == 0 {
		return ""
	}
	dimmed := m.styles.Agent.Done
	completed := m.styles.Agent.Completed
	w := m.width
	if w < 10 {
		w = 10
	}
	var b strings.Builder
	for i, a := range m.runningAgents {
		if i > 0 {
			b.WriteByte('\n')
		}
		done := !a.DoneAt.IsZero()
		// Line 1: dot [Agent] description  model | turn N | in/out/cache | tool
		var dot string
		if done {
			if a.Status == "completed" {
				dot = completed.Render("✓")
			} else {
				dot = dimmed.Render("✗")
			}
		} else {
			dot = "●"
			if m.statusFrame%4 >= 2 {
				dot = "○"
			}
			dot = m.styles.Agent.Label.Render(dot)
		}
		descStyle := dimmed
		if !done {
			descStyle = m.styles.Agent.Label
		}
		label := dot + " " + descStyle.Render("[Agent]") + " " + descStyle.Render(a.Description)
		var meta []string
		if a.Model != "" {
			meta = append(meta, a.Model)
		}
		if a.TurnCount > 0 {
			meta = append(meta, fmt.Sprintf("turn %d", a.TurnCount))
		}
		if a.InputTokens > 0 || a.OutputTokens > 0 {
			parts := []string{}
			if a.InputTokens > 0 {
				parts = append(parts, "in "+formatTokenK(a.InputTokens))
			}
			if a.OutputTokens > 0 {
				parts = append(parts, "out "+formatTokenK(a.OutputTokens))
			}
			if a.CacheReadTokens > 0 {
				parts = append(parts, "cache "+formatTokenK(a.CacheReadTokens))
			}
			meta = append(meta, strings.Join(parts, " "))
		}
		if !done && a.ToolCall != "" {
			meta = append(meta, a.ToolCall)
		}
		if len(meta) > 0 {
			label += "  " + dimmed.Render(strings.Join(meta, " | "))
		}
		b.WriteString(ansi.Truncate(label, w, "…"))
		// Line 2: streaming text or done summary
		b.WriteByte('\n')
		var msg string
		if done {
			if a.Status == "completed" {
				msg = completed.Render("done")
			} else {
				msg = dimmed.Render("failed")
			}
		} else {
			msg = a.LastMsg
			if msg == "" {
				msg = "…"
			}
			msg = dimmed.Render(msg)
		}
		b.WriteString(ansi.Truncate("  "+msg, w, "…"))
	}
	return b.String()
}
