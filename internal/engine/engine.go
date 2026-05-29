package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"cece/internal/agent"
	"cece/internal/logger"
	"cece/internal/prompt"
	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/tool"
)

const defaultKeepRecentTurns = 2

// Engine is the core agent engine. It manages conversation state, dispatches
// user input to the agent loop, and emits protocol.Events on a channel.
//
// Engine implements agent.TurnEngine (for TurnBootstrap) and satisfies the
// ui.Sender / ui.Actor / ui.Eventer interfaces consumed by the BubbleTea UI.
type Engine struct {
	mu                sync.Mutex
	client            agent.ModelClient
	registry          *tool.Registry
	assembler         *prompt.ContextAssembler
	projectDir        string
	planState         *tool.PlanModeState
	taskList          *tool.TaskList
	history           []agent.Message
	cancel            context.CancelFunc
	confirmCh         chan struct{} // set per Input call, cleared on completion
	yolo              bool          // auto-approve tool execution without UI confirmation
	maxTokens         int           // configurable max output tokens
	toolResultPolicy  agent.ToolResultPolicy
	ContextWindowFor  func(model string) int // returns context window for a model ID
	store             session.Store          // optional persistence backend
	sessionID         string                 // current session ID, empty = not yet created
	sessionCreated    bool                   // true after first Input creates a session
	modelName         string                 // current model name for meta persistence
	contextWindow     int                    // current context window size for meta persistence
	protocol          string                 // current protocol (anthropic, aiden, codebase, etc.)
	configName        string                 // current provider config name
	lastInputTokens   int                    // last request input tokens for resume water level
	totalInputTokens  int                    // cumulative input tokens across turns
	totalOutputTokens int                    // cumulative output tokens across turns
	apiCalls          int                    // cumulative API call count
	toolCounts        map[string]int         // cumulative tool execution counts
	cacheReadTokens   int                    // cumulative cache read tokens
	cacheCreationTokens int                  // cumulative cache creation tokens
	inputQueue        *userInputQueue        // queued user inputs while agent is busy
	questionAnswers   []tool.QuestionAnswer
	eventCh           chan protocol.Event // global event channel for async responses
}

func NewEngine(client agent.ModelClient, registry *tool.Registry, yolo bool, maxTokens int, assembler *prompt.ContextAssembler, projectDir string) *Engine {
	return &Engine{
		client:           client,
		registry:         registry,
		assembler:        assembler,
		projectDir:       projectDir,
		planState:        tool.NewPlanModeState(),
		taskList:         tool.NewTaskList(),
		yolo:             yolo,
		maxTokens:        maxTokens,
		toolResultPolicy: agent.ToolResultPolicy{InlineMaxLines: 200, HeadLines: 80, TailLines: 80},
		inputQueue:       &userInputQueue{},
		toolCounts:       make(map[string]int),
		eventCh:          make(chan protocol.Event, 4096),
	}
}

// ── TurnEngine interface implementation ───────────────────────────────────

func (e *Engine) ProjectDir() string                      { return e.projectDir }
func (e *Engine) Assembler() *prompt.ContextAssembler     { return e.assembler }
func (e *Engine) Client() agent.ModelClient                { return e.client }
func (e *Engine) Registry() *tool.Registry                { return e.registry }
func (e *Engine) PlanState() *tool.PlanModeState          { return e.planState }
func (e *Engine) TaskList() *tool.TaskList               { return e.taskList }

// SetMCPTools replaces all MCP tools in the registry.
// It removes any tool whose name starts with "mcp_" then adds the given tools.
func (e *Engine) SetMCPTools(tools []tool.Tool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registry.SetMCPTools(tools)
}
func (e *Engine) Yolo() bool                              { return e.yolo }
func (e *Engine) MaxTokens() int                          { return e.maxTokens }
func (e *Engine) ToolResultPolicy() agent.ToolResultPolicy { return e.toolResultPolicy }
func (e *Engine) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionID
}
func (e *Engine) HistoryLen() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.history)
}

func (e *Engine) AppendHistory(msg agent.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, msg)
}

func (e *Engine) PersistMessage(ctx context.Context, msg agent.Message) {
	agent.NewSessionCoordinator(e.store).PersistMessage(ctx, e.SessionID(), msg)
}

func (e *Engine) HistorySnapshot() []agent.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]agent.Message, len(e.history))
	copy(out, e.history)
	return out
}

func (e *Engine) SetLastInputTokens(tokens int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastInputTokens = tokens
}

func (e *Engine) IncrementTokens(input, output int) (sessionID string, meta session.SessionMeta, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sessionID == "" {
		return "", session.SessionMeta{}, false
	}
	e.totalInputTokens += input
	e.totalOutputTokens += output
	return e.sessionID, session.SessionMeta{
		Model:             e.modelName,
		ContextWindow:     e.contextWindow,
		Protocol:          e.protocol,
		ConfigName:        e.configName,
		LastInputTokens:   e.lastInputTokens,
		TotalInputTokens:  e.totalInputTokens,
		TotalOutputTokens: e.totalOutputTokens,
		StatusBar:         e.statusBarSnapshotLocked(),
	}, true
}

func (e *Engine) ResetQuestionAnswers() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.questionAnswers = nil
}

func (e *Engine) GetQuestionAnswers() []tool.QuestionAnswer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tool.QuestionAnswer(nil), e.questionAnswers...)
}

func (e *Engine) DrainQueuedInputs() []string {
	return e.inputQueue.Drain()
}

// ── Public API ─────────────────────────────────────────────────────────────

// Events returns the global event channel for async query responses.
func (e *Engine) Events() <-chan protocol.Event {
	return e.eventCh
}

// emitEvent sends a protocol.Event to the global event channel.
func (e *Engine) emitEvent(ev protocol.Event) {
	e.eventCh <- ev
}

// EmitEvent sends a protocol.Event to the global event channel.
// Exported for use by EngineMediator.
func (e *Engine) EmitEvent(ev protocol.Event) {
	e.eventCh <- ev
}

func (e *Engine) PlanModeState() *tool.PlanModeState {
	return e.planState
}

func (e *Engine) SetPlanModeState(state *tool.PlanModeState) {
	if state == nil {
		state = tool.NewPlanModeState()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.planState = state
}

func (e *Engine) SetTaskList(tl *tool.TaskList) {
	if tl == nil {
		tl = tool.NewTaskList()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.taskList = tl
}

func (e *Engine) Mode() protocol.PermissionMode {
	if e.planState == nil {
		return protocol.PermissionModeDefault
	}
	return protocol.PermissionMode(e.planState.Mode())
}

func (e *Engine) PlanMode() protocol.PermissionMode {
	return e.Mode()
}

func (e *Engine) SetToolResultPolicy(policy agent.ToolResultPolicy) {
	e.toolResultPolicy = agent.NormalizeToolResultPolicy(policy)
}

func (e *Engine) SetStore(store session.Store) {
	e.store = store
}

// SetClient replaces the underlying ModelClient. Used by the mediator
// when switching models across protocols.
func (e *Engine) SetClient(client agent.ModelClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.client = client
}

func (e *Engine) SetModelInfo(model string, contextWindow int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modelName = model
	e.contextWindow = contextWindow
}

func (e *Engine) GetTokenCounts() (input, output int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.totalInputTokens, e.totalOutputTokens
}

func (e *Engine) SessionMeta() (model string, contextWindow, lastInput, totalInput, totalOutput int, protocol string, configName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelName, e.contextWindow, e.lastInputTokens, e.totalInputTokens, e.totalOutputTokens, e.protocol, e.configName
}

// ResetModelInfo sets the model tracking fields. Used by the mediator
// after session load.
func (e *Engine) ResetModelInfo(model string, cw int, proto string, cn string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modelName = model
	e.contextWindow = cw
	e.protocol = proto
	e.configName = cn
}

// SetTokenState sets the token tracking fields. Used by the mediator
// after session load to restore water-level data.
func (e *Engine) SetTokenState(lastInput, totalInput, totalOutput int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastInputTokens = lastInput
	e.totalInputTokens = totalInput
	e.totalOutputTokens = totalOutput
}

// IncrementAPICalls increments the API call counter.
func (e *Engine) IncrementAPICalls() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.apiCalls++
}

// IncrementToolCount increments the tool execution counter for the given tool.
func (e *Engine) IncrementToolCount(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.toolCounts == nil {
		e.toolCounts = make(map[string]int)
	}
	e.toolCounts[name]++
}

// UpdateCacheTokens updates cumulative cache token counts.
func (e *Engine) UpdateCacheTokens(read, creation int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cacheReadTokens += read
	e.cacheCreationTokens += creation
}

// StatusBarSnapshot returns the current status bar data for persistence.
func (e *Engine) StatusBarSnapshot() session.StatusBarSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.statusBarSnapshotLocked()
}

// statusBarSnapshotLocked returns a snapshot assuming the mutex is already held.
func (e *Engine) statusBarSnapshotLocked() session.StatusBarSnapshot {
	tc := make(map[string]int, len(e.toolCounts))
	for k, v := range e.toolCounts {
		tc[k] = v
	}
	return session.StatusBarSnapshot{
		APICalls:            e.apiCalls,
		ToolCounts:          tc,
		CacheReadTokens:     e.cacheReadTokens,
		CacheCreationTokens: e.cacheCreationTokens,
	}
}

// SetStatusBarState restores status bar counters from a snapshot (used on session load).
func (e *Engine) SetStatusBarState(sb session.StatusBarSnapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.apiCalls = sb.APICalls
	if sb.ToolCounts != nil {
		e.toolCounts = make(map[string]int, len(sb.ToolCounts))
		for k, v := range sb.ToolCounts {
			e.toolCounts[k] = v
		}
	} else {
		e.toolCounts = make(map[string]int)
	}
	e.cacheReadTokens = sb.CacheReadTokens
	e.cacheCreationTokens = sb.CacheCreationTokens
}

// toolCountsSnapshot returns a copy of the tool counts map (thread-safe).
func (e *Engine) toolCountsSnapshot() map[string]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.toolCounts) == 0 {
		return nil
	}
	tc := make(map[string]int, len(e.toolCounts))
	for k, v := range e.toolCounts {
		tc[k] = v
	}
	return tc
}

// LoadHistory replaces the conversation history from loaded messages.
func (e *Engine) LoadHistory(ctx context.Context, sessionID string, msgs []agent.Message) {
	e.mu.Lock()
	e.history = msgs
	e.sessionID = sessionID
	e.sessionCreated = true
	logger.SetSessionID(sessionID)
	e.mu.Unlock()
}

// ClearHistory clears all conversation history and token counters, then
// notifies the UI via HistoryClearedEvent.
func (e *Engine) ClearHistory() {
	e.mu.Lock()
	e.history = nil
	// Keep token counters across clears so status bar statistics persist.
	e.mu.Unlock()
	e.emitEvent(protocol.HistoryClearedEvent{})
}

// CompactHistory compresses conversation history by summarizing older messages.
// It inserts a CompactBoundary message into history. When building API requests,
// MessagesAfterCompactBoundary skips everything before the boundary.
// Old messages are preserved in history for UI scrollback.
func (e *Engine) CompactHistory(ctx context.Context) {
	e.mu.Lock()
	snapshot := make([]agent.Message, len(e.history))
	copy(snapshot, e.history)
	client := e.client
	e.mu.Unlock()

	// Only compact messages after the last boundary.
	// Pre-boundary messages were already summarized and shouldn't be re-sent.
	compactable := agent.MessagesAfterCompactBoundary(snapshot)

	slog.Info("compact started", "history_len", len(snapshot), "compactable", len(compactable))
	e.emitEvent(protocol.CompactingEvent{})

	compactor := agent.NewCompactor(client, defaultKeepRecentTurns)
	result, err := compactor.Compact(ctx, compactable)
	if err != nil {
		slog.Error("compact failed", "error", err)
		e.emitEvent(protocol.CompactedEvent{
			MessagesBefore: len(snapshot),
			MessagesAfter:  len(snapshot),
		})
		return
	}

	if result.SummarizeCount == 0 {
		e.emitEvent(protocol.CompactedEvent{
			MessagesBefore: len(snapshot),
			MessagesAfter:  len(snapshot),
		})
		return
	}

	// Append boundary message to history (old messages preserved for UI scrollback)
	e.mu.Lock()
	e.history = append(e.history, result.Boundary)
	historyLen := len(e.history)
	sessionID := e.sessionID
	e.mu.Unlock()

	// Persist boundary message to session JSONL
	if sessionID != "" {
		e.PersistMessage(context.Background(), result.Boundary)
	}

	e.emitEvent(protocol.CompactedEvent{
		TokensBefore:   result.TokensBefore,
		TokensAfter:    result.TokensAfter,
		MessagesBefore: len(snapshot),
		MessagesAfter:  historyLen - result.SummarizeCount + 1,
		Summary:        result.Boundary.Content,
	})
}

func (e *Engine) isSessionCreated() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionCreated
}

func (e *Engine) History() []protocol.Message {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]protocol.Message, len(e.history))
	for i, m := range e.history {
		out[i] = agent.MessageToDTO(m)
	}
	return out
}

func (e *Engine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.inputQueue.Clear()
}

// QueueInput appends a user input to the queue while the agent is busy.
func (e *Engine) QueueInput(input string) {
	e.inputQueue.Append(input)
}

// QueuedInputCount returns the number of queued user inputs.
func (e *Engine) QueuedInputCount() int {
	return e.inputQueue.Len()
}

// ClearQueuedInputs removes all queued user inputs.
func (e *Engine) ClearQueuedInputs() {
	e.inputQueue.Clear()
}

// Confirm signals the Engine to proceed with pending tool execution.
func (e *Engine) Confirm() {
	e.mu.Lock()
	ch := e.confirmCh
	e.mu.Unlock()
	if ch != nil {
		ch <- struct{}{}
	}
}

// ApprovePlan approves the plan and allows the ExitPlanMode tool to execute.
func (e *Engine) ApprovePlan() { e.Confirm() }

// RejectPlan rejects the plan and cancels the current turn.
func (e *Engine) RejectPlan() { e.Cancel() }

// Do dispatches A-class protocol actions (agent-loop interrupt signals).
// B-class actions (SwitchModel, LoadSession, ListModels, CycleMode) are
// handled by EngineMediator.
func (e *Engine) Do(action protocol.Action) {
	switch a := action.(type) {
	case protocol.ConfirmAction:
		e.Confirm()
	case protocol.CancelAction:
		e.Cancel()
	case protocol.ApprovePlanAction:
		e.ApprovePlan()
	case protocol.RejectPlanAction:
		e.RejectPlan()
	case protocol.AnswerQuestionAction:
		e.AnswerQuestion(a.Answers)
	case protocol.QueueInputAction:
		e.QueueInput(a.Text)
	case protocol.ClearHistoryAction:
		e.ClearHistory()
	case protocol.SetPermissionModeAction:
		e.setMode(a.Mode)
	case protocol.SetExitTargetModeAction:
		if ps := e.PlanModeState(); ps != nil {
			ps.SetExitTargetMode(tool.PermissionMode(a.Mode))
		}
	}
}

func (e *Engine) setMode(mode protocol.PermissionMode) {
	ps := e.PlanModeState()
	if ps == nil {
		ps = tool.NewPlanModeState()
		e.SetPlanModeState(ps)
	}
	nextMode := ps.SetMode(tool.PermissionMode(mode))
	e.emitModeChanged(nextMode)
}

func (e *Engine) emitModeChanged(mode tool.PermissionMode) {
	var displayText string
	switch mode {
	case tool.PermissionModeAutoAccept:
		displayText = "Auto-accept mode"
	case tool.PermissionModePlan:
		displayText = "Entered plan mode"
	default:
		displayText = "Default mode"
	}
	e.emitEvent(protocol.ModeChangedEvent{Mode: protocol.PermissionMode(mode), Message: displayText})
}

// AnswerQuestion stores the user's answers and signals the agent loop to continue.
func (e *Engine) AnswerQuestion(answers []protocol.QuestionAnswer) {
	e.mu.Lock()
	e.questionAnswers = agent.DtoAnswersToInternal(answers)
	e.mu.Unlock()
	e.Confirm()
}

func (e *Engine) Input(ctx context.Context, input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return errors.New("input must not be empty")
	}

	slog.Info("send called", "input", input)

	ctx, cancel := context.WithCancel(ctx)

	user := agent.Message{Role: agent.UserRole, Content: input}
	snapshot := e.beginInputTurn(user)

	sessionCoordinator := agent.NewSessionCoordinator(e.store)
	newSession := sessionCoordinator.StartTurn(ctx, input, e.SessionID(), e.isSessionCreated())
	if newSession.ID != "" {
		e.mu.Lock()
		e.sessionID = newSession.ID
		e.sessionCreated = true
		e.mu.Unlock()
	}
	e.PersistMessage(ctx, user)

	events := make(chan agent.Event)
	confirmCh := make(chan struct{}, 1)

	e.mu.Lock()
	e.cancel = cancel
	e.confirmCh = confirmCh
	e.mu.Unlock()

	go func() {
		defer close(events)
		defer func() {
			e.mu.Lock()
			e.cancel = nil
			e.confirmCh = nil
			e.mu.Unlock()
		}()

		agent.NewTurnBootstrap(e, sessionCoordinator, confirmCh).Run(ctx, input, user, snapshot, newSession, events)
	}()

	// Bridge internal events to protocol events via the unified event bus.
	// Inject cumulative status bar data into relevant events.
	go func() {
		for ev := range events {
			d := agent.ToDTO(ev)
			if d == nil {
				continue
			}
			// Inject Engine-managed cumulative counters into protocol events.
			switch v := d.(type) {
			case protocol.ModelRequestStarted:
				v.APICalls = e.apiCalls
				d = v
			case protocol.ToolExecCompleted:
				v.ToolCounts = e.toolCountsSnapshot()
				d = v
			}
			e.emitEvent(d)
		}
		e.emitEvent(protocol.TurnCompleted{})
	}()

	return nil
}

func (e *Engine) beginInputTurn(user agent.Message) []agent.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, user)
	// Build snapshot: only messages after the last compact boundary
	raw := agent.MessagesAfterCompactBoundary(e.history)
	snapshot := make([]agent.Message, len(raw))
	copy(snapshot, raw)
	// Inject plan mode reminder if active.
	if e.planState != nil && e.planState.Mode() == tool.PermissionModePlan {
		reminderType := e.planState.ReminderType()
		plansDir := e.planState.PlansDir()
		slog.Info("plan mode injection check", "reminderType", reminderType, "plansDir", plansDir)
		switch reminderType {
		case "full":
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildFullPlanReminder(plansDir)})
			e.planState.SetReminderType("sparse")
		case "sparse":
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildSparsePlanReminder(plansDir)})
		}
	}

	snapshot = agent.EnsureToolResultCoverage(snapshot)
	return snapshot
}

// ── userInputQueue ─────────────────────────────────────────────────────────

type userInputQueue struct {
	mu    sync.Mutex
	items []string
}

func (q *userInputQueue) Append(input string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, input)
}

func (q *userInputQueue) Drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

func (q *userInputQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *userInputQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
}
