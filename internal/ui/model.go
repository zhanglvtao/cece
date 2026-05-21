package ui

import (
	"context"
	"fmt"
	"image"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cece/internal/chat"
	"cece/internal/ui/dialog"
	"cece/internal/ui/list"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/x/ansi"
)

// ── Focus states ────────────────────────────────────────────────────────────

type focusState uint8

const (
	focusEditor focusState = iota
	focusChat
)

// ── Messages ────────────────────────────────────────────────────────────────

type runtimeEventMsg struct{ event chat.Event }

type sendResultMsg struct {
	events <-chan chat.Event
	err    error
}

type streamClosedMsg struct{}

type spinnerTickMsg struct{}

type modelsLoadedMsg struct {
	models []chat.ModelInfo
	err    error
}

// Sender interface ────────────────────────────────────────────────────────

type Sender interface {
	Input(ctx context.Context, input string) (<-chan chat.Event, error)
}

// Confirmer is implemented by the Runtime to allow the UI to signal
// tool execution approval or cancellation.
type Confirmer interface {
	Confirm()
	Cancel()
}

// ModelSwitcher is implemented by Runtime to support model switching.
type ModelSwitcher interface {
	SwitchModel(model string, maxContextWindow int, apiKey string, baseURL string, authMode string, authHelper string, protocol string, configName string)
}

// AllModelLister is implemented by Runtime to list models from all providers.
type AllModelLister interface {
	ListAllModels(ctx context.Context) ([]chat.ModelInfo, error)
}

// ── Model (root UI) ─────────────────────────────────────────────────────────

// Model is the root bubbletea v2 model. It composes Header, Chat, Input, and
// Dialog Overlay into a three-section vertical layout.
type Model struct {
	sender    Sender
	modelName string
	styles    Styles
	keyMap    KeyMap

	// Sub-components
	chat    *Chat
	input   *Input
	overlay *dialog.Overlay

	// Layout state
	focus  focusState
	busy   bool
	status string
	width  int
	height int

	// Header data
	gitBranch     string
	workDir       string
	contextWindow int

	// Streaming
	events       <-chan chat.Event
	spinnerFrame int

	// Mouse state: when the user presses a button inside the chat area
	// we treat it as the start of a text-selection gesture and suppress
	// wheel scrolling until the button is released. This lets users drag
	// to select a range without the screen scrolling out from under them.
	chatSelecting bool
}

// NewModel creates a new root UI model.
func NewModel(sender Sender, modelName string, projectDir string, contextWindow ...int) Model {
	styles := DefaultStyles()
	keyMap := DefaultKeyMap()

	chatComp := NewChat(styles)
	inputComp := NewInput(styles)
	inputComp.SetPromptStyle()

	branch := gitBranch(projectDir)
	wd := filepath.Base(projectDir)
	cw := 0
	if len(contextWindow) > 0 {
		cw = contextWindow[0]
	}

	return Model{
		sender:        sender,
		modelName:     modelName,
		styles:        styles,
		keyMap:        keyMap,
		chat:          chatComp,
		input:         inputComp,
		overlay:       dialog.NewOverlay(),
		focus:         focusEditor,
		status:        "Ready",
		gitBranch:     branch,
		workDir:       wd,
		contextWindow: cw,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.update(msg)
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to dialog overlay if active.
	if m.overlay.HasDialogs() {
		action := m.overlay.Update(msg)
		return m, m.handleDialogAction(action)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.handleResize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseMotionMsg:
		// Motion events: forward to input only if the input box is the
		// active gesture target. Otherwise drop them so native terminal
		// selection (Shift+drag in Ghostty/iTerm2/Terminal) stays clean.
		if m.isInInputArea(msg.X, msg.Y) {
			cmd := m.input.Update(msg)
			return m, cmd
		}
		return m, nil

	case sendResultMsg:
		if msg.err != nil {
			m.busy = false
			m.status = msg.err.Error()
			return m, nil
		}
		m.events = msg.events
		return m, waitEventCmd(m.events)

	case runtimeEventMsg:
		m.applyEvent(msg.event)
		if m.events != nil {
			return m, waitEventCmd(m.events)
		}
		return m, nil

	case streamClosedMsg:
		return m, nil

	case spinnerTickMsg:
		if m.busy {
			m.spinnerFrame++
			m.chat.AdvanceLoading()
			return m, spinnerTickCmd()
		}

	case modelsLoadedMsg:
		if msg.err != nil {
			m.status = "Failed to load models"
			return m, nil
		}
		dialogStyles := dialog.DefaultDialogStyles()
		picker := dialog.NewModelPicker(dialogStyles, msg.models, m.modelName)
		m.overlay.OpenDialog(picker)
		m.status = "Select model"
		return m, nil
	}

	// Forward other messages to the input (e.g. cursor blink).
	cmd := m.input.Update(msg)
	return m, cmd
}

// applyEvent applies a chat event and updates model-level state.
func (m *Model) applyEvent(event chat.Event) {
	// Skip duplicate UIUserMessageAdded — already shown by handleSend.
	if _, ok := event.(chat.UIUserMessageAdded); ok && m.busy {
		return
	}
	m.chat.ApplyEvent(event)
	switch event.(type) {
	case chat.UIModelRequestStarted:
		m.busy = true
		m.status = "Requesting"
	case chat.UIAssistantStarted:
		m.busy = true
		m.status = "Streaming"
	case chat.UIAssistantCompleted:
		m.busy = false
		m.status = "Ready"
	case chat.UIRunFailed:
		m.busy = false
		m.status = "Cancelled"
	case chat.UITruncationRetry:
		m.status = "Retrying (64K)..."
	case chat.UIToolCallsReady:
		var infos []dialog.ToolCallInfo
		for _, call := range event.(chat.UIToolCallsReady).Calls {
			argPreview := string(call.Input)
			if len(argPreview) > 40 {
				argPreview = argPreview[:37] + "..."
			}
			infos = append(infos, dialog.ToolCallInfo{
				Name: call.Name,
				Args: argPreview,
			})
		}
		m.overlay.OpenDialog(dialog.NewConfirm(dialog.DefaultDialogStyles(), infos))
		m.status = "Confirm tools"
	}
}

// View implements tea.Model (v2 API).
func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	// Mouse tracking: cell-motion mode is the minimum that delivers wheel
	// events. We route mouse messages by cursor position in update():
	// chat area → wheel scrolls history; input area → wheel/click/release
	// are forwarded to the textarea. A click inside the chat area arms a
	// "selecting" flag that suppresses wheel scrolling until release, so
	// drag-to-select gestures aren't disrupted by inertial scrolling.
	// Native terminal selection still works via Shift+drag in
	// Ghostty/iTerm2/Apple Terminal, which bypasses application capture.
	v.MouseMode = tea.MouseModeCellMotion

	scr := uv.NewScreenBuffer(m.width, m.height)
	cursor := m.drawScreen(scr, scr.Bounds())

	content := scr.Render()
	v.Content = content
	v.Cursor = cursor
	return v
}

func (m *Model) drawScreen(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	chatRect, inputRect, statusBarRect := m.generateLayout(area.Dx(), area.Dy())

	// Chat
	m.chat.Draw(scr, chatRect)

	// Input
	m.input.Draw(scr, inputRect)

	// Status Bar
	drawStatusBar(scr, statusBarRect, m.styles, StatusBarData{
		Status:        m.status,
		Model:         m.modelName,
		GitBranch:     m.gitBranch,
		WorkDir:       m.workDir,
		InputTokens:   func() int { in, _ := m.chat.TokenInfo(); return in }(),
		OutputTokens:  func() int { _, out := m.chat.TokenInfo(); return out }(),
		ContextUsed:   m.chat.ContextUsed(),
		ContextWindow: m.contextWindow,
		Busy:          m.busy,
	})

	// Dialog overlay
	if m.overlay.HasDialogs() {
		return m.overlay.Draw(scr, area)
	}

	// Return cursor for the editor
	if m.focus == focusEditor {
		cur := m.input.Cursor()
		if cur != nil {
			cur.X += inputRect.Min.X
			cur.Y += inputRect.Min.Y
			return cur
		}
	}
	return nil
}

// ── Key handling ────────────────────────────────────────────────────────────

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Quit):
		if m.busy {
			if canceler, ok := m.sender.(interface{ Cancel() }); ok {
				canceler.Cancel()
			}
			m.busy = false
			m.status = "Cancelled"
			return m, nil
		}
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.Cancel):
		if m.busy {
			if canceler, ok := m.sender.(interface{ Cancel() }); ok {
				canceler.Cancel()
			}
			m.busy = false
			m.status = "Cancelled"
			return m, nil
		}
		if m.focus == focusChat {
			m.focus = focusEditor
			m.chat.Blur()
			m.input.Focus()
			return m, nil
		}
		return m, tea.Quit

	case key.Matches(msg, m.keyMap.Sessions):
		return m, m.openSessionsDialog()

	case key.Matches(msg, m.keyMap.SwitchFocus):
		if m.focus == focusEditor {
			m.focus = focusChat
			m.input.Blur()
			m.chat.Focus()
		} else {
			m.focus = focusEditor
			m.chat.Blur()
			m.input.Focus()
		}
		return m, nil

	case key.Matches(msg, m.keyMap.Editor.Send) && !m.busy:
		return m.handleSend()

	case key.Matches(msg, m.keyMap.Editor.Newline):
		m.input.InsertRune('\n')
		return m, nil
	}

	// Focus-dependent key handling
	switch m.focus {
	case focusEditor:
		return m.handleEditorKey(msg)
	case focusChat:
		return m.handleChatKey(msg)
	}
	return m, nil
}

func (m *Model) handleEditorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Editor.HistoryUp):
		m.input.HistoryUp()
		cmd := m.input.Update(msg)
		return m, cmd
	case key.Matches(msg, m.keyMap.Editor.HistoryDown):
		m.input.HistoryDown()
		cmd := m.input.Update(msg)
		return m, cmd
	default:
		cmd := m.input.Update(msg)
		return m, cmd
	}
}

func (m *Model) handleChatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keyMap.Chat.Up):
		if !m.chat.SelectPrev() {
			m.chat.ScrollBy(-1)
		}
	case key.Matches(msg, m.keyMap.Chat.Down):
		if !m.chat.SelectNext() {
			m.chat.ScrollBy(1)
		}
	case key.Matches(msg, m.keyMap.Chat.PageUp):
		m.chat.ScrollBy(-m.chat.Height())
	case key.Matches(msg, m.keyMap.Chat.PageDown):
		m.chat.ScrollBy(m.chat.Height())
	case key.Matches(msg, m.keyMap.Chat.Home):
		m.chat.list.ScrollToTop()
	case key.Matches(msg, m.keyMap.Chat.End):
		m.chat.ScrollToBottom()
	case key.Matches(msg, m.keyMap.Chat.Expand):
		m.chat.ToggleExpand()
	default:
		// Any unhandled key in chat mode returns focus to editor
		m.focus = focusEditor
		m.chat.Blur()
		m.input.Focus()
		cmd := m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── Send handling ───────────────────────────────────────────────────────────

func (m *Model) handleSend() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input.Value())
	if input == "" {
		return m, nil
	}

	// Slash command interception
	if strings.HasPrefix(input, "/") {
		m.input.Reset()
		return m.handleSlashCommand(input)
	}

	m.input.Reset()
	m.input.AddHistory(input)

	// Immediately show user message in chat (before LLM response)
	m.chat.ApplyEvent(chat.UIUserMessageAdded{
		Message: chat.Message{Role: chat.UserRole, Content: input},
	})
	loading := &loadingItem{
		Versioned: list.NewVersioned(),
		styles:    m.styles,
		label:     "Thinking",
		startAt:   time.Now(),
	}
	m.chat.SetLoading(loading)
	m.busy = true
	m.status = "Submitting"

	return m, tea.Batch(submitCmd(m.sender, input), spinnerTickCmd())
}

func (m *Model) handleSlashCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.SplitN(input, " ", 2)
	cmd := parts[0]
	switch cmd {
	case "/model":
		m.status = "Loading models..."
		return m, m.openModelPicker()
	default:
		m.status = "Unknown command: " + cmd
		return m, nil
	}
}

func (m *Model) openModelPicker() tea.Cmd {
	lister, ok := m.sender.(AllModelLister)
	if !ok {
		return func() tea.Msg { return modelsLoadedMsg{err: fmt.Errorf("model listing not supported")} }
	}
	return func() tea.Msg {
		models, err := lister.ListAllModels(context.Background())
		return modelsLoadedMsg{models: models, err: err}
	}
}

// ── Mouse handling ──────────────────────────────────────────────────────────

// handleMouseWheel routes wheel events by cursor position:
//   - inside the chat area → scrolls the chat history.
//   - inside the input area → forwards to the textarea (cursor up/down,
//     which moves the textarea viewport with it).
//
// While a click-drag selection gesture is in progress in the chat area
// (chatSelecting), wheel events are suppressed so the screen does not
// scroll out from under the user mid-selection.
//
// Dialogs never reach this handler — `update()` delegates to the overlay
// while it has dialogs.
func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.isInChatArea(msg.X, msg.Y):
		if m.chatSelecting {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			m.chat.ScrollBy(-3)
		case tea.MouseWheelDown:
			m.chat.ScrollBy(3)
		}
	case m.isInInputArea(msg.X, msg.Y):
		switch msg.Button {
		case tea.MouseWheelUp:
			m.input.ScrollBy(-3)
		case tea.MouseWheelDown:
			m.input.ScrollBy(3)
		}
	}
	return m, nil
}

// handleMouseClick records the start of a potential text-selection gesture
// when the click lands inside the chat area, and forwards clicks inside the
// input area to the textarea (so it can position its cursor).
func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.isInChatArea(msg.X, msg.Y):
		m.chatSelecting = true
	case m.isInInputArea(msg.X, msg.Y):
		cmd := m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleMouseRelease ends the selection gesture in the chat area and
// forwards releases inside the input area to the textarea.
func (m *Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.chatSelecting {
		m.chatSelecting = false
	}
	if m.isInInputArea(msg.X, msg.Y) {
		cmd := m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// isInChatArea reports whether (x, y) lies within the chat history rectangle.
func (m *Model) isInChatArea(x, y int) bool {
	if m.width <= 0 || m.height <= 0 {
		return false
	}
	chatRect, _, _ := m.generateLayout(m.width, m.height)
	return image.Pt(x, y).In(image.Rectangle(chatRect))
}

// isInInputArea reports whether (x, y) lies within the input box rectangle.
func (m *Model) isInInputArea(x, y int) bool {
	if m.width <= 0 || m.height <= 0 {
		return false
	}
	_, inputRect, _ := m.generateLayout(m.width, m.height)
	return image.Pt(x, y).In(image.Rectangle(inputRect))
}

// ── Dialog handling ─────────────────────────────────────────────────────────

func (m *Model) openSessionsDialog() tea.Cmd {
	sessions := m.listSessions()
	dialogStyles := dialog.DefaultDialogStyles()
	d := dialog.NewSessions(dialogStyles, sessions, "")
	m.overlay.OpenDialog(d)
	return nil
}

func (m *Model) handleDialogAction(action dialog.Action) tea.Cmd {
	if action == nil {
		return nil
	}
	switch a := action.(type) {
	case dialog.ActionClose:
		m.overlay.CloseFrontDialog()
	case dialog.ActionSelectSession:
		m.overlay.CloseFrontDialog()
		// TODO: implement session switching via chat.Runtime
		_ = a.ID
	case dialog.ActionCmd:
		return a.Cmd
	case dialog.ActionConfirmTools:
		m.overlay.CloseFrontDialog()
		if confirmer, ok := m.sender.(Confirmer); ok {
			confirmer.Confirm()
		}
	case dialog.ActionRejectTools:
		m.overlay.CloseFrontDialog()
		if confirmer, ok := m.sender.(Confirmer); ok {
			confirmer.Cancel()
		}
		m.busy = false
		m.status = "Tool calls rejected"
	case dialog.ActionSelectModel:
		m.overlay.CloseFrontDialog()
		m.modelName = a.ID
		if a.MaxContextWindow > 0 {
			m.contextWindow = a.MaxContextWindow
		}
		if sw, ok := m.sender.(ModelSwitcher); ok {
			sw.SwitchModel(a.ID, a.MaxContextWindow, a.APIKey, a.BaseURL, a.AuthMode, a.AuthHelper, a.Protocol, "")
		}
		m.status = a.Provider + "/" + a.DisplayName
	}
	return nil
}

func (m *Model) listSessions() []dialog.SessionInfo {
	// TODO: integrate with chat.Runtime session storage
	return nil
}

// ── Layout ──────────────────────────────────────────────────────────────────

func (m *Model) generateLayout(w, h int) (chat_, input, statusbar uv.Rectangle) {
	const statusBarHeight = 1
	inputHeight := m.input.Height() // already includes separator line

	layout.Vertical(
		layout.Fill(1),
		layout.Len(inputHeight),
		layout.Len(statusBarHeight),
	).Split(uv.Rect(0, 0, w, h)).Assign(&chat_, &input, &statusbar)

	return chat_, input, statusbar
}

func (m *Model) handleResize(w, h int) {
	chatRect, inputRect, _ := m.generateLayout(w, h)
	m.chat.SetSize(chatRect.Dx(), chatRect.Dy())
	m.input.SetWidth(inputRect.Dx())
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func gitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func submitCmd(sender Sender, input string) tea.Cmd {
	return func() tea.Msg {
		events, err := sender.Input(context.Background(), input)
		return sendResultMsg{events: events, err: err}
	}
}

func waitEventCmd(events <-chan chat.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return streamClosedMsg{}
		}
		return runtimeEventMsg{event: event}
	}
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// Suppress unused import warnings.
var _ ansi.MouseButton
