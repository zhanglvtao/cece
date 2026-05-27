package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"cece/internal/chat"
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
// Engine implements chat.TurnEngine (for TurnBootstrap) and satisfies the
// ui.Sender / ui.Actor / ui.Eventer interfaces consumed by the BubbleTea UI.
type Engine struct {
	mu                sync.Mutex
	client            chat.ModelClient
	registry          *tool.Registry
	assembler         *prompt.ContextAssembler
	projectDir        string
	planState         *tool.PlanModeState
	history           []chat.Message
	cancel            context.CancelFunc
	confirmCh         chan struct{} // set per Input call, cleared on completion
	yolo              bool          // auto-approve tool execution without UI confirmation
	maxTokens         int           // configurable max output tokens
	toolResultPolicy  chat.ToolResultPolicy
	ContextWindowFor  func(model string) int // returns context window for a model ID
	store             session.Store           // optional persistence backend
	sessionID         string                  // current session ID, empty = not yet created
	sessionCreated    bool                    // true after first Input creates a session
	modelName         string                  // current model name for meta persistence
	contextWindow     int                     // current context window size for meta persistence
	protocol          string                  // current protocol (anthropic, aiden, codebase, etc.)
	configName        string                  // current provider config name
	lastInputTokens   int                     // last request input tokens for resume water level
	totalInputTokens  int                     // cumulative input tokens across turns
	totalOutputTokens int                     // cumulative output tokens across turns
	inputQueue        *userInputQueue         // queued user inputs while agent is busy
	questionAnswers   []tool.QuestionAnswer
	eventCh           chan protocol.Event // global event channel for async responses
}

func NewEngine(client chat.ModelClient, registry *tool.Registry, yolo bool, maxTokens int, assembler *prompt.ContextAssembler, projectDir string) *Engine {
	return &Engine{
		client:           client,
		registry:         registry,
		assembler:        assembler,
		projectDir:       projectDir,
		planState:        tool.NewPlanModeState(),
		yolo:             yolo,
		maxTokens:        maxTokens,
		toolResultPolicy: chat.ToolResultPolicy{InlineMaxLines: 200, HeadLines: 80, TailLines: 80},
		inputQueue:       &userInputQueue{},
		eventCh:          make(chan protocol.Event, 4096),
	}
}

// ── TurnEngine interface implementation ───────────────────────────────────

func (e *Engine) ProjectDir() string                     { return e.projectDir }
func (e *Engine) Assembler() *prompt.ContextAssembler     { return e.assembler }
func (e *Engine) Client() chat.ModelClient               { return e.client }
func (e *Engine) Registry() *tool.Registry               { return e.registry }
func (e *Engine) PlanState() *tool.PlanModeState          { return e.planState }
func (e *Engine) Yolo() bool                             { return e.yolo }
func (e *Engine) MaxTokens() int                         { return e.maxTokens }
func (e *Engine) ToolResultPolicy() chat.ToolResultPolicy { return e.toolResultPolicy }
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

func (e *Engine) AppendHistory(msg chat.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, msg)
}

func (e *Engine) PersistMessage(ctx context.Context, msg chat.Message) {
	chat.NewSessionCoordinator(e.store).PersistMessage(ctx, e.SessionID(), msg)
}

func (e *Engine) HistorySnapshot() []chat.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]chat.Message, len(e.history))
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

func (e *Engine) Mode() protocol.PermissionMode {
	if e.planState == nil {
		return protocol.PermissionModeDefault
	}
	return protocol.PermissionMode(e.planState.Mode())
}

func (e *Engine) PlanMode() protocol.PermissionMode {
	return e.Mode()
}

func (e *Engine) SetToolResultPolicy(policy chat.ToolResultPolicy) {
	e.toolResultPolicy = chat.NormalizeToolResultPolicy(policy)
}

func (e *Engine) SetStore(store session.Store) {
	e.store = store
}

// SetClient replaces the underlying ModelClient. Used by the mediator
// when switching models across protocols.
func (e *Engine) SetClient(client chat.ModelClient) {
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

// LoadHistory replaces the conversation history from loaded messages.
func (e *Engine) LoadHistory(ctx context.Context, sessionID string, msgs []chat.Message) {
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
	e.totalInputTokens = 0
	e.totalOutputTokens = 0
	e.lastInputTokens = 0
	e.mu.Unlock()
	e.emitEvent(protocol.HistoryClearedEvent{})
}

// CompactHistory compresses conversation history by summarizing older messages.
// It uses the current model client to generate a summary and preserves the
// most recent turns verbatim.
func (e *Engine) CompactHistory(ctx context.Context) {
	e.mu.Lock()
	if len(e.history) < 4 { // need at least 2 turns to compact
		e.mu.Unlock()
		return
	}
	snapshot := make([]chat.Message, len(e.history))
	copy(snapshot, e.history)
	client := e.client
	e.mu.Unlock()

	e.emitEvent(protocol.CompactingEvent{})

	compactor := chat.NewCompactor(client, defaultKeepRecentTurns)
	result, err := compactor.Compact(ctx, snapshot)
	if err != nil {
		slog.Error("compact failed", "error", err)
		e.emitEvent(protocol.CompactedEvent{
			MessagesBefore: len(snapshot),
			MessagesAfter:  len(snapshot),
		})
		return
	}

	e.mu.Lock()
	e.history = result.Messages
	e.mu.Unlock()

	e.emitEvent(protocol.CompactedEvent{
		TokensBefore:   result.TokensBefore,
		TokensAfter:    result.TokensAfter,
		MessagesBefore: result.MessagesBefore,
		MessagesAfter:  result.MessagesAfter,
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
		out[i] = chat.MessageToDTO(m)
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
	case protocol.CompactAction:
		go e.CompactHistory(context.Background())
	}
}

// AnswerQuestion stores the user's answers and signals the agent loop to continue.
func (e *Engine) AnswerQuestion(answers []protocol.QuestionAnswer) {
	e.mu.Lock()
	e.questionAnswers = chat.DtoAnswersToInternal(answers)
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

	user := chat.Message{Role: chat.UserRole, Content: input}
	snapshot := e.beginInputTurn(user)

	sessionCoordinator := chat.NewSessionCoordinator(e.store)
	newSession := sessionCoordinator.StartTurn(ctx, input, e.SessionID(), e.isSessionCreated())
	if newSession.ID != "" {
		e.mu.Lock()
		e.sessionID = newSession.ID
		e.sessionCreated = true
		e.mu.Unlock()
	}
	e.PersistMessage(ctx, user)

	events := make(chan chat.Event)
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

		chat.NewTurnBootstrap(e, sessionCoordinator, confirmCh).Run(ctx, input, user, snapshot, newSession, events)
	}()

	// Bridge internal events to protocol events via the unified event bus.
	go func() {
		for ev := range events {
			if d := chat.ToDTO(ev); d != nil {
				e.emitEvent(d)
			}
		}
		e.emitEvent(protocol.TurnCompleted{})
	}()

	return nil
}

func (e *Engine) beginInputTurn(user chat.Message) []chat.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, user)
	snapshot := make([]chat.Message, len(e.history))
	copy(snapshot, e.history)
	// Inject plan mode reminder if active.
	if e.planState != nil && e.planState.Mode() == tool.PermissionModePlan {
		reminderType := e.planState.ReminderType()
		plansDir := e.planState.PlansDir()
		slog.Info("plan mode injection check", "reminderType", reminderType, "plansDir", plansDir)
		switch reminderType {
		case "full":
			snapshot = append(snapshot, chat.Message{Role: chat.UserRole, Content: tool.BuildFullPlanReminder(plansDir)})
			e.planState.SetReminderType("sparse")
		case "sparse":
			snapshot = append(snapshot, chat.Message{Role: chat.UserRole, Content: tool.BuildSparsePlanReminder(plansDir)})
		}
	}
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
