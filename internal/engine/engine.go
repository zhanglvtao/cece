package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/effort"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/prompt"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

// Engine is the core agent engine. It manages conversation state, dispatches
// user input to the agent loop, and emits protocol.Events on a channel.
//
// Engine implements agent.TurnEngine (for TurnBootstrap) and satisfies the
// ui.Sender / ui.Actor / ui.Eventer interfaces consumed by the BubbleTea UI.
// SubAgentRuntimeFactory builds a complete AgentRuntime for a sub-agent.
// This is injected by runtime.Build so the Engine doesn't need to know
// about provider resolvers, MCP managers, etc.
type SubAgentRuntimeFactory interface {
	NewSubAgentRuntime(ctx context.Context, cfg SubAgentBuildConfig) (*AgentRuntime, error)
}

// SubAgentBuildConfig carries the parameters for building a sub-agent runtime.
type SubAgentBuildConfig struct {
	AgentID           string
	Description       string
	Model             string
	ProjectDir        string
	MaxTokens         int
	MaxTurns          int // 0 = unlimited
	ParentSessionID   string
	SystemPromptExtra string
	Tools             []string // explicit tool names; empty = all except Agent
}

type AgentController interface {
	Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error)
	CancelAll(parent *Engine)
}

type agentControllerFunc func(context.Context, *Engine, tool.AgentSubAgentConfig, tool.Emitter) (tool.AgentSubAgentResult, error)

func (f agentControllerFunc) Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
	return f(ctx, parent, cfg, emitter)
}

func (f agentControllerFunc) CancelAll(parent *Engine) {}

type Engine struct {
	mu         sync.Mutex
	client     agent.ModelClient
	registry   *tool.Registry
	assembler  *prompt.ContextAssembler
	projectDir string
	planState  *tool.PlanModeState
	taskList   *tool.TaskList
	history    []agent.Message
	cancel     context.CancelFunc
	confirmCh  chan struct{} // set per Input call, cleared on completion
	rejectCh   chan struct{} // set per Input call, signals user rejection without cancel
	yolo       bool          // auto-approve tool execution without UI confirmation
	maxTokens  int           // configurable max output tokens
	effort     string        // reasoning effort: "low", "medium", "high", "xhigh", "auto"

	ContextWindowFor           func(model string) int               // returns context window for a model ID
	ModelClientFor             func(model string) agent.ModelClient // returns ModelClient for a model ID, nil = use current client
	store                      session.Store                        // optional persistence backend
	sessionID                  string                               // current session ID, empty = not yet created
	sessionCreated             bool                                 // true after first Input creates a session
	modelName                  string                               // current model name for meta persistence
	contextWindow              int                                  // current context window size for meta persistence
	protocol                   string                               // current protocol (anthropic, aiden, codebase, etc.)
	configName                 string                               // current provider config name
	lastInputTokens            int                                  // last request input tokens for resume water level
	totalInputTokens           int                                  // cumulative input tokens across turns
	totalOutputTokens          int                                  // cumulative output tokens across turns
	apiCalls                   int                                  // cumulative API call count
	toolCounts                 map[string]int                       // cumulative tool execution counts (success + failure)
	failedToolCounts           map[string]int                       // cumulative tool failure counts
	turnCount                  int                                  // cumulative conversation turn count
	cacheReadTokens            int                                  // cumulative cache read tokens
	cacheCreationTokens        int                                  // cumulative cache creation tokens
	lastCompactTurn            int                                  // turn count at last compact/prune
	consecutiveCompactFailures int                                  // circuit breaker: stop autoCompact after 3 failures
	lastNudgeTurn              int                                  // last turn number when nudge was injected (throttle)
	inputQueue                 *userInputQueue                      // queued user inputs while agent is busy
	questionAnswers            []tool.QuestionAnswer
	agentController            AgentController
	eventCh                    chan protocol.Event // global event channel for async responses
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
		inputQueue:       &userInputQueue{},
		toolCounts:       make(map[string]int),
		failedToolCounts: make(map[string]int),
		eventCh:          make(chan protocol.Event, 4096),
	}
}

// ── TurnEngine interface implementation ───────────────────────────────────

func (e *Engine) ProjectDir() string { return e.projectDir }

const subAgentResultPreviewMaxLen = 16000

func (e *Engine) Assembler() *prompt.ContextAssembler { return e.assembler }
func (e *Engine) Client() agent.ModelClient           { return e.client }
func (e *Engine) Registry() *tool.Registry            { return e.registry }
func (e *Engine) PlanState() *tool.PlanModeState      { return e.planState }
func (e *Engine) TaskList() *tool.TaskList            { return e.taskList }

// SetMCPTools replaces all MCP tools in the registry.
// It removes any tool whose name starts with "mcp_" then adds the given tools.
func (e *Engine) SetMCPTools(tools []tool.Tool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registry.SetMCPTools(tools)
}
func (e *Engine) Yolo() bool     { return e.yolo }
func (e *Engine) MaxTokens() int { return e.maxTokens }
func (e *Engine) Effort() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.effort
}

// SetEffort configures the reasoning effort level.
func (e *Engine) SetEffort(v string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.effort = v
}

// resolveEffort returns the concrete effort level for a turn.
// If e.effort is empty or "auto", it uses keyword-based selection on the input.
// Sub-agents always get Low.
func (e *Engine) resolveEffort(isSubAgent bool, input string) effort.ReasoningEffort {
	e.mu.Lock()
	v := e.effort
	e.mu.Unlock()
	return effort.Resolve(effort.ReasoningEffort(v), isSubAgent, input)
}
func (e *Engine) ContextWindow() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.contextWindow
}
func (e *Engine) ToolResultPolicy() agent.ToolResultPolicy { return agent.ToolResultPolicy{} }
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
	return safeRequestHistory(out)
}

func safeRequestHistory(messages []agent.Message) []agent.Message {
	raw := agent.MessagesAfterCompactBoundary(messages)
	snapshot := make([]agent.Message, len(raw))
	copy(snapshot, raw)
	return agent.ValidateToolResultCoverage(agent.EnsureToolResultCoverage(snapshot))
}

// ReplaceHistory replaces the entire conversation history with the given messages.
func (e *Engine) ReplaceHistory(messages []agent.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = messages
}

// CompactHandler returns a tool.CompactHandler backed by this engine.
func (e *Engine) CompactHandler() *tool.CompactHandler {
	return &tool.CompactHandler{
		Summary: e.compactSummary,
	}
}

// TrimToolResultsHandler returns a tool.TrimToolResultsHandler backed by this engine.
func (e *Engine) TrimToolResultsHandler() *tool.TrimToolResultsHandler {
	return &tool.TrimToolResultsHandler{
		TrimToolResults: e.compactTrimToolResults,
	}
}

// PruneHandler returns a tool.PruneHandler backed by this engine.
func (e *Engine) PruneHandler() *tool.PruneHandler {
	return &tool.PruneHandler{
		Prune: e.compactPrune,
	}
}

func (e *Engine) compactSummary(ctx context.Context, keepTurn int) (string, int, int, error) {
	e.mu.Lock()
	snapshot := make([]agent.Message, len(e.history))
	copy(snapshot, e.history)
	client := e.client
	e.mu.Unlock()

	e.emitEvent(protocol.CompactingEvent{})

	boundaries := agent.TurnBoundaries(snapshot)
	totalTurns := len(boundaries)

	if totalTurns == 0 {
		return "", 0, 0, fmt.Errorf("no turns to summarize")
	}

	// Resolve keepTurn
	if keepTurn < 0 {
		keepTurn = totalTurns - 2
		if keepTurn < 1 {
			keepTurn = 1
		}
	}
	if keepTurn >= totalTurns {
		return "", 0, 0, fmt.Errorf("turn %d is beyond the last turn (%d)", keepTurn, totalTurns-1)
	}

	splitIdx, ok := agent.SafeContextBoundaryBeforeTurn(snapshot, keepTurn)
	if !ok {
		return "", 0, 0, fmt.Errorf("turn %d has no safe user boundary", keepTurn)
	}
	summarize := snapshot[:splitIdx]
	keep := snapshot[splitIdx:]

	if len(summarize) == 0 {
		return "", 0, 0, fmt.Errorf("no messages to summarize before turn %d", keepTurn)
	}

	// Only count tokens for API-visible messages (after last compact boundary).
	// Full history includes pre-boundary messages retained for UI scrollback,
	// which inflates the estimate far beyond what the API actually sees.
	visible := agent.MessagesAfterCompactBoundary(snapshot)
	tokensBefore := agent.EstimateMessagesTokens(visible)

	compactor := agent.NewCompactor(client, 0)
	summary, err := compactor.GenerateSummary(ctx, summarize)
	if err != nil {
		return "", 0, 0, err
	}

	boundary := agent.Message{
		Role: agent.UserRole,
		Content: fmt.Sprintf(
			"This session is being continued from a previous conversation. The summary below covers turns 0–%d.\n\n%s\n\nTurns %d onward are preserved verbatim.",
			keepTurn-1, summary, keepTurn,
		),
		CompactBoundary: true,
	}

	// Insert boundary between summarized and kept messages.
	// Old (summarized) messages are preserved for UI scrollback and persistence;
	// MessagesAfterCompactBoundary skips everything before the boundary for API requests.
	newHistory := make([]agent.Message, 0, len(snapshot)+1)
	newHistory = append(newHistory, snapshot[:splitIdx]...)
	newHistory = append(newHistory, boundary)
	newHistory = append(newHistory, keep...)
	tokensAfter := agent.EstimateMessagesTokens(append([]agent.Message{boundary}, keep...))

	e.mu.Lock()
	e.history = newHistory
	sessionID := e.sessionID
	e.lastCompactTurn = len(agent.TurnBoundaries(newHistory))
	e.mu.Unlock()

	if sessionID != "" {
		e.PersistMessage(context.Background(), boundary)
	}

	e.emitEvent(protocol.CompactedEvent{
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		MessagesBefore: len(snapshot),
		MessagesAfter:  len(newHistory),
		Summary:        summary,
	})

	return summary, tokensBefore, tokensAfter, nil
}

func (e *Engine) compactTrimToolResults(fromTurn, toTurn int) (int, int, int) {
	e.mu.Lock()
	history := e.history // mutate in place
	e.mu.Unlock()

	// Locate the last compact boundary to find the API-visible offset.
	// Turn indices from the tool are relative to visible messages,
	// so we offset them into the full history.
	visible := agent.MessagesAfterCompactBoundary(history)
	offset := len(history) - len(visible)

	boundaries := agent.TurnBoundaries(visible)
	totalTurns := len(boundaries)

	if toTurn > totalTurns {
		toTurn = totalTurns
	}
	if fromTurn >= toTurn {
		return 0, 0, 0
	}

	// Token estimates should reflect only API-visible messages,
	// not the full history which includes pre-boundary scrollback.
	tokensBefore := agent.EstimateMessagesTokens(visible)

	// Trim on the full history so mutations are reflected in e.history.
	// Use offset-adjusted turn indices so the correct range is trimmed.
	truncatedCount, _, _ := agent.TrimToolResultsInRange(history, fromTurn+offset, toTurn+offset)

	// TrimToolResultsInRange mutates in place, so re-derive visible and estimate after.
	visible = agent.MessagesAfterCompactBoundary(history)
	tokensAfter := agent.EstimateMessagesTokens(visible)

	e.mu.Lock()
	e.history = history
	e.mu.Unlock()

	e.emitEvent(protocol.TruncatedToolResultsEvent{
		TruncatedCount: truncatedCount,
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
	})

	return truncatedCount, tokensBefore, tokensAfter
}

func (e *Engine) compactPrune(turn int) (int, int) {
	e.mu.Lock()
	snapshot := make([]agent.Message, len(e.history))
	copy(snapshot, e.history)
	e.mu.Unlock()

	boundaries := agent.TurnBoundaries(snapshot)
	// Only count tokens for API-visible messages (after last compact boundary).
	// Full history includes pre-boundary messages retained for UI scrollback,
	// which inflates the estimate far beyond what the API actually sees.
	visible := agent.MessagesAfterCompactBoundary(snapshot)
	tokensBefore := agent.EstimateMessagesTokens(visible)

	if turn <= 0 || turn >= len(boundaries) {
		return tokensBefore, tokensBefore
	}

	startIdx, ok := agent.SafeContextBoundaryBeforeTurn(snapshot, turn)
	if !ok {
		return tokensBefore, tokensBefore
	}

	boundary := agent.Message{
		Role: agent.UserRole,
		Content: fmt.Sprintf(
			"Context pruned: %d messages across %d turns before this point have been removed to free context. Continue the conversation based on what remains.",
			startIdx, turn,
		),
		CompactBoundary: true,
	}

	// Insert boundary at the prune point, keeping old messages for UI scrollback.
	newHistory := make([]agent.Message, 0, len(snapshot)+1)
	newHistory = append(newHistory, snapshot[:startIdx]...)
	newHistory = append(newHistory, boundary)
	newHistory = append(newHistory, snapshot[startIdx:]...)
	// tokensAfter: only count what's API-visible after the new boundary.
	tokensAfter := agent.EstimateMessagesTokens(agent.MessagesAfterCompactBoundary(newHistory))

	e.mu.Lock()
	e.history = newHistory
	sessionID := e.sessionID
	e.lastCompactTurn = len(agent.TurnBoundaries(newHistory))
	e.mu.Unlock()

	if sessionID != "" {
		e.PersistMessage(context.Background(), boundary)
	}

	e.emitEvent(protocol.PrunedEvent{
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		MessagesBefore: len(snapshot),
		MessagesAfter:  len(newHistory),
		PrunedTurns:    turn,
	})

	return tokensBefore, tokensAfter
}

// AgentHandler returns a tool.AgentHandler backed by this engine.
func (e *Engine) AgentHandler() *tool.AgentHandler {
	return &tool.AgentHandler{
		RunSubAgent: func(ctx context.Context, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
			e.mu.Lock()
			controller := e.agentController
			e.mu.Unlock()
			if controller == nil {
				return tool.AgentSubAgentResult{
					Status:  string(AgentStatusFailed),
					Content: "agent controller is not configured",
					Err:     "agent_controller_not_configured",
				}, nil
			}
			return controller.Run(ctx, e, cfg, emitter)
		},
	}
}

// writeSubAgentArtifact persists the full final result as an artifact
// and updates the result with preview / artifact path metadata.
func (e *Engine) writeSubAgentArtifact(result tool.AgentSubAgentResult, rt *AgentRuntime) tool.AgentSubAgentResult {
	as, ok := e.store.(session.ArtifactStore)
	if !ok {
		return result
	}
	full := rt.finalResult
	if full == "" {
		full = result.Content
	}
	if full == "" {
		return result
	}

	path, err := as.WriteArtifact(context.Background(), result.SessionID, "result.txt", []byte(full))
	if err != nil {
		slog.Warn("failed to write subagent artifact", "session", result.SessionID, "error", err)
		return result
	}

	result.ResultPath = path
	result.ContentFullLength = len(full)
	if len(full) > subAgentResultPreviewMaxLen {
		result.Content = full[:subAgentResultPreviewMaxLen]
		result.ContentReturnedLength = subAgentResultPreviewMaxLen
		result.ContentTruncated = true
	} else {
		result.Content = full
		result.ContentReturnedLength = len(full)
	}
	slog.Info("subagent: artifact written",
		"sessionID", result.SessionID,
		"path", path,
		"fullLen", result.ContentFullLength,
		"returnedLen", result.ContentReturnedLength,
		"truncated", result.ContentTruncated,
	)
	return result
}

// accumulateSubAgentTokens adds completed sub-agent token counts to the parent session.
func (e *Engine) accumulateSubAgentTokens(result tool.AgentSubAgentResult) {
	if result.Status == string(AgentStatusCompleted) || result.Status == string(AgentStatusFailed) || result.Status == string(AgentStatusCancelled) {
		e.mu.Lock()
		e.totalInputTokens += result.InputTokens
		e.totalOutputTokens += result.OutputTokens
		e.apiCalls += result.TurnsUsed + 1
		e.mu.Unlock()
	}
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

func (e *Engine) SetStore(store session.Store) {
	e.store = store
}

func (e *Engine) SetAgentController(controller AgentController) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentController = controller
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

// SessionMetaModel returns just the model name from session meta.
func (e *Engine) SessionMetaModel() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelName
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

// ── Context Nudge ─────────────────────────────────────────────────────────

const nudgeContextPctThreshold = 75 // minimum context % used to trigger nudge

// shouldNudge checks whether a context-pressure nudge should be injected.
// Returns (shouldNudge, turnsSinceCompact, contextPct, contextWindow).
// Only contextPct >= nudgeContextPctThreshold triggers the nudge.
// Throttled to at most once per turn.
func (e *Engine) shouldNudge() (bool, int, int, int) {
	e.mu.Lock()
	history := e.history
	lastCompactTurn := e.lastCompactTurn
	lastInputTokens := e.lastInputTokens
	contextWindow := e.contextWindow
	turnCount := e.turnCount
	lastNudgeTurn := e.lastNudgeTurn
	e.mu.Unlock()

	totalTurns := len(agent.TurnBoundaries(history))
	turnsSinceCompact := totalTurns - lastCompactTurn
	contextPct := 0
	if contextWindow > 0 {
		contextPct = lastInputTokens * 100 / contextWindow
	}

	if contextWindow <= 0 || lastInputTokens <= 0 {
		slog.Debug("nudge check skipped",
			"reason", "zero contextWindow or lastInputTokens",
			"contextWindow", contextWindow,
			"lastInputTokens", lastInputTokens,
			"totalTurns", totalTurns,
		)
		return false, turnsSinceCompact, contextPct, contextWindow
	}

	// Throttle: at most one nudge per turn.
	if turnCount == lastNudgeTurn {
		slog.Debug("nudge check: already nudged this turn",
			"turnCount", turnCount,
		)
		return false, turnsSinceCompact, contextPct, contextWindow
	}

	if contextPct < nudgeContextPctThreshold {
		slog.Debug("nudge check: context usage too low",
			"contextPct", contextPct,
			"threshold", nudgeContextPctThreshold,
			"turnsSinceCompact", turnsSinceCompact,
			"lastInputTokens", lastInputTokens,
			"contextWindow", contextWindow,
		)
		return false, turnsSinceCompact, contextPct, contextWindow
	}

	slog.Info("nudge triggered",
		"contextPct", contextPct,
		"turnsSinceCompact", turnsSinceCompact,
		"lastInputTokens", lastInputTokens,
		"contextWindow", contextWindow,
		"totalTurns", totalTurns,
	)
	return true, turnsSinceCompact, contextPct, contextWindow
}

// buildContextNudgeReminder returns the reminder used when context pressure is high.
func buildContextNudgeReminder(contextPct, usedK, windowK, turnsSinceCompact int) string {
	return fmt.Sprintf(
		"<system-reminder>\nContext pressure: %d%% used (%dK/%dK), %d turns since last context management.\nManage context as needed. Use Compact, TrimToolResults, or Prune based on what best fits the current state.\n</system-reminder>",
		contextPct, usedK, windowK, turnsSinceCompact,
	)
}

// injectNudge appends a context-pressure system-reminder to the snapshot
// to nudge the LLM toward context management. Returns the modified snapshot.
func (e *Engine) injectNudge(snapshot []agent.Message, turnsSinceCompact, contextPct, contextWindow int) []agent.Message {
	usedK := (e.lastInputTokens + 999) / 1000
	windowK := (contextWindow + 999) / 1000
	nudgeText := buildContextNudgeReminder(contextPct, usedK, windowK, turnsSinceCompact)
	slog.Info("injecting context nudge into snapshot", "contextPct", contextPct, "usedK", usedK, "windowK", windowK, "turnsSinceCompact", turnsSinceCompact)
	snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: nudgeText})

	// Record that we nudged this turn so shouldNudge won't fire again.
	e.mu.Lock()
	e.lastNudgeTurn = e.turnCount
	e.mu.Unlock()

	return snapshot
}

// IncrementAPICalls increments the API call counter.
func (e *Engine) IncrementAPICalls() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.apiCalls++
}

// ── Auto Compact ──────────────────────────────────────────────────────────

const autoCompactPctThreshold = 90      // autoCompact triggers at 90% context usage
const maxConsecutiveCompactFailures = 3 // circuit breaker threshold

// shouldAutoCompact checks whether the system should automatically compact
// based on context usage. Returns true when lastInputTokens >= 90% of
// contextWindow and the circuit breaker hasn't tripped.
func (e *Engine) shouldAutoCompact() bool {
	e.mu.Lock()
	lastInputTokens := e.lastInputTokens
	contextWindow := e.contextWindow
	failures := e.consecutiveCompactFailures
	e.mu.Unlock()

	if contextWindow <= 0 || lastInputTokens <= 0 {
		return false
	}

	if failures >= maxConsecutiveCompactFailures {
		slog.Debug("autoCompact skipped: circuit breaker tripped",
			"consecutiveFailures", failures,
		)
		return false
	}

	contextPct := lastInputTokens * 100 / contextWindow
	if contextPct < autoCompactPctThreshold {
		return false
	}

	slog.Info("autoCompact triggered",
		"contextPct", contextPct,
		"lastInputTokens", lastInputTokens,
		"contextWindow", contextWindow,
	)
	return true
}

// TryAutoCompact checks if auto-compact is needed and executes it.
// Returns true if compact was performed (history was modified).
func (e *Engine) TryAutoCompact(ctx context.Context) bool {
	if !e.shouldAutoCompact() {
		return false
	}

	e.CompactHistory(ctx)

	e.mu.Lock()
	failures := e.consecutiveCompactFailures
	e.mu.Unlock()

	if failures >= maxConsecutiveCompactFailures {
		slog.Warn("autoCompact circuit breaker tripped, stopping auto-compact attempts",
			"consecutiveFailures", failures,
		)
	}

	return true
}

// RecordToolExecution records a tool execution, incrementing both the total
// count and the failure count when isError is true.
func (e *Engine) RecordToolExecution(name string, isError bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.toolCounts == nil {
		e.toolCounts = make(map[string]int)
	}
	if e.failedToolCounts == nil {
		e.failedToolCounts = make(map[string]int)
	}
	e.toolCounts[name]++
	if isError {
		e.failedToolCounts[name]++
	}
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
	fc := make(map[string]int, len(e.failedToolCounts))
	for k, v := range e.failedToolCounts {
		fc[k] = v
	}
	return session.StatusBarSnapshot{
		APICalls:            e.apiCalls,
		ToolCounts:          tc,
		ToolFailedCounts:    fc,
		CacheReadTokens:     e.cacheReadTokens,
		CacheCreationTokens: e.cacheCreationTokens,
		TurnCount:           e.turnCount,
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
	e.turnCount = sb.TurnCount
	if sb.ToolFailedCounts != nil {
		e.failedToolCounts = make(map[string]int, len(sb.ToolFailedCounts))
		for k, v := range sb.ToolFailedCounts {
			e.failedToolCounts[k] = v
		}
	} else {
		e.failedToolCounts = make(map[string]int)
	}
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

// SessionStats returns cumulative session statistics for external drivers.
func (e *Engine) SessionStats() protocol.SessionStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	success := make(map[string]int, len(e.toolCounts))
	for k, v := range e.toolCounts {
		success[k] = v - e.failedToolCounts[k]
	}
	failed := make(map[string]int, len(e.failedToolCounts))
	for k, v := range e.failedToolCounts {
		failed[k] = v
	}
	return protocol.SessionStats{
		TurnCount:           e.turnCount,
		APICalls:            e.apiCalls,
		TotalInputTokens:    e.totalInputTokens,
		TotalOutputTokens:   e.totalOutputTokens,
		CacheReadTokens:     e.cacheReadTokens,
		CacheCreationTokens: e.cacheCreationTokens,
		LastInputTokens:     e.lastInputTokens,
		ToolSuccessCounts:   success,
		ToolFailedCounts:    failed,
	}
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

	compactor := agent.NewCompactor(client, 0)
	result, err := compactor.Compact(ctx, compactable)
	if err != nil {
		slog.Error("compact failed", "error", err)
		e.mu.Lock()
		e.consecutiveCompactFailures++
		slog.Warn("compact failure count", "consecutiveFailures", e.consecutiveCompactFailures)
		e.mu.Unlock()
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

	// Insert boundary at the split point between summarized and kept messages.
	// compactable starts at offset (len(snapshot) - len(compactable)) in the full history.
	// The split within compactable is at result.SummarizeCount.
	compactableOffset := len(snapshot) - len(compactable)
	insertIdx := compactableOffset + result.SummarizeCount

	newHistory := make([]agent.Message, 0, len(snapshot)+1)
	newHistory = append(newHistory, snapshot[:insertIdx]...)
	newHistory = append(newHistory, result.Boundary)
	newHistory = append(newHistory, snapshot[insertIdx:]...)

	e.mu.Lock()
	e.history = newHistory
	sessionID := e.sessionID
	e.consecutiveCompactFailures = 0 // reset circuit breaker on success
	e.mu.Unlock()

	// Persist boundary message to session JSONL
	if sessionID != "" {
		e.PersistMessage(context.Background(), result.Boundary)
	}

	e.emitEvent(protocol.CompactedEvent{
		TokensBefore:   result.TokensBefore,
		TokensAfter:    result.TokensAfter,
		MessagesBefore: len(snapshot),
		MessagesAfter:  len(newHistory),
		Summary:        result.Boundary.Content,
	})
}

// TruncateToolResults truncates all tool_result content in conversation history
// to "[truncated]". Zero API cost, irreversible.
func (e *Engine) TruncateToolResults() {
	e.mu.Lock()
	truncatedCount, tokensBefore, tokensAfter := agent.TruncateToolResults(e.history)
	e.mu.Unlock()

	e.emitEvent(protocol.TruncatedToolResultsEvent{
		TruncatedCount: truncatedCount,
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
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
	controller := e.agentController
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.inputQueue.Clear()
	e.mu.Unlock()
	if controller != nil {
		controller.CancelAll(e)
	}
}

// QueueInput appends a user input to the queue while the agent is busy.
func (e *Engine) QueueInput(input string) {
	e.mu.Lock()
	idle := e.cancel == nil
	e.mu.Unlock()
	if idle {
		if err := e.Input(context.Background(), input); err == nil {
			return
		}
	}
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

// PopLastQueuedInput removes and returns the last queued input.
func (e *Engine) PopLastQueuedInput() (string, bool) {
	return e.inputQueue.PopLast()
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

// RejectPlan rejects the plan by signalling the interaction gate.
func (e *Engine) RejectPlan() { e.signalReject() }

// RejectToolCalls rejects the pending tool calls by signalling the interaction gate.
func (e *Engine) RejectToolCalls() { e.signalReject() }

// RejectQuestion cancels the question by signalling the interaction gate.
func (e *Engine) RejectQuestion() { e.signalReject() }

// signalReject sends a rejection signal on the rejectCh channel.
func (e *Engine) signalReject() {
	e.mu.Lock()
	ch := e.rejectCh
	e.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

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
	case protocol.RejectToolCallsAction:
		e.RejectToolCalls()
	case protocol.RejectQuestionAction:
		e.RejectQuestion()
	case protocol.AnswerQuestionAction:
		e.AnswerQuestion(a.Answers)
	case protocol.QueueInputAction:
		e.QueueInput(a.Text)
	case protocol.ClearHistoryAction:
		e.ClearHistory()
	case protocol.TruncateToolResultsAction:
		e.TruncateToolResults()
	case protocol.SetPermissionModeAction:
		e.setMode(a.Mode)
	case protocol.SetExitTargetModeAction:
		if ps := e.PlanModeState(); ps != nil {
			ps.SetExitTargetMode(tool.PermissionMode(a.Mode))
		}
	case protocol.DryRunRequestAction:
		e.DryRunRequest(a.Input)
	case protocol.AppendShellResultAction:
		e.appendShellResult(a)
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

// appendShellResult appends a shell command's user message and output to the
// conversation history. ANSI color codes are stripped from the output before
// persisting, since they're only meaningful in TUI display.
func (e *Engine) appendShellResult(a protocol.AppendShellResultAction) {
	ctx := context.Background()
	userMsg := agent.Message{Role: agent.UserRole, Content: "!" + a.Command}
	e.AppendHistory(userMsg)
	e.PersistMessage(ctx, userMsg)

	prefix := "Shell output"
	if a.IsError {
		prefix = "Shell error"
	}
	// Strip ANSI escape codes before persisting — colors are only for TUI.
	cleanOutput := ansi.Strip(a.Output)
	assistantMsg := agent.Message{Role: agent.AssistantRole, Content: prefix + ":\n" + cleanOutput}
	e.AppendHistory(assistantMsg)
	e.PersistMessage(ctx, assistantMsg)
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

	// Resolve reasoning effort and apply to client.
	resolvedEffort := e.resolveEffort(false, input)
	if e.client != nil {
		e.client.SetReasoningEffort(string(resolvedEffort))
	}
	e.emitEvent(protocol.EffortChangedEvent{Effort: string(resolvedEffort)})

	ctx, cancel := context.WithCancel(ctx)

	user := agent.Message{Role: agent.UserRole, Content: input}

	snapshot := e.beginInputTurn(user)

	// Check if a context-pressure nudge should be injected.
	if ok, turns, pct, cw := e.shouldNudge(); ok {
		snapshot = e.injectNudge(snapshot, turns, pct, cw)
		e.emitEvent(protocol.ContextNudgedEvent{
			TurnsSinceCompact: turns,
			ContextPct:        pct,
			ContextUsed:       e.lastInputTokens,
			ContextWindow:     cw,
		})
	}

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
	rejectCh := make(chan struct{}, 1)

	e.mu.Lock()
	e.cancel = cancel
	e.confirmCh = confirmCh
	e.rejectCh = rejectCh
	e.mu.Unlock()

	go func() {
		defer close(events)
		defer func() {
			e.mu.Lock()
			e.cancel = nil
			e.confirmCh = nil
			e.rejectCh = nil
			e.mu.Unlock()
		}()

		agent.NewTurnBootstrap(e, sessionCoordinator, confirmCh, rejectCh).Run(ctx, input, user, snapshot, newSession, events)
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
				v.ContextWindow = e.contextWindow
				d = v
			case protocol.ToolExecCompleted:
				v.ToolCounts = e.toolCountsSnapshot()
				d = v
			}
			e.emitEvent(d)
		}
		sb := e.StatusBarSnapshot()
		e.mu.Lock()
		e.turnCount++
		e.mu.Unlock()
		e.emitEvent(protocol.TurnCompleted{
			LastInputTokens:     e.lastInputTokens,
			TotalInputTokens:    e.totalInputTokens,
			TotalOutputTokens:   e.totalOutputTokens,
			CacheReadTokens:     sb.CacheReadTokens,
			CacheCreationTokens: sb.CacheCreationTokens,
			ContextWindow:       e.contextWindow,
			TurnCount:           e.turnCount,
		})
	}()

	return nil
}

func (e *Engine) DryRunRequest(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		input = "<dryrun>"
	}
	user := agent.Message{Role: agent.UserRole, Content: input}
	snapshot := e.previewInputTurn(user)
	bootstrap := agent.NewTurnBootstrap(e, agent.NewSessionCoordinator(e.store), nil, nil)
	plan := bootstrap.BuildTurnPlan(input, snapshot)
	e.emitEvent(agent.ToDTO(bootstrap.BuildDryRunRequest(input, plan)))
}

func (e *Engine) beginInputTurn(user agent.Message) []agent.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, user)
	return buildTurnSnapshot(e.history[:len(e.history)-1], user, e.planState, true)
}

func (e *Engine) previewInputTurn(user agent.Message) []agent.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return buildTurnSnapshot(e.history, user, e.planState, false)
}

func buildTurnSnapshot(history []agent.Message, user agent.Message, planState *tool.PlanModeState, consumePlanReminder bool) []agent.Message {
	all := append(append([]agent.Message(nil), history...), user)
	snapshot := safeRequestHistory(all)
	if planState != nil && planState.Mode() == tool.PermissionModePlan {
		reminderType := planState.ReminderType()
		plansDir := planState.PlansDir()
		slog.Info("plan mode injection check", "reminderType", reminderType, "plansDir", plansDir)
		switch reminderType {
		case "full":
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildFullPlanReminder(plansDir, planState.HasWritingPlanSkill())})
			if consumePlanReminder {
				planState.SetReminderType("sparse")
			}
		case "sparse":
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildSparsePlanReminder(plansDir, planState.HasWritingPlanSkill())})
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

func (q *userInputQueue) PopLast() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return "", false
	}
	last := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return last, true
}

// ── Test helpers ───────────────────────────────────────────────────────────

// SetNudgeStateForTest sets internal state to trigger context nudge.
// For testing only.
func (e *Engine) SetNudgeStateForTest(lastInputTokens int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastInputTokens = lastInputTokens
}

// HistoryForTest returns a copy of the conversation history.
// For testing only.
func (e *Engine) HistoryForTest() []agent.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]agent.Message, len(e.history))
	copy(out, e.history)
	return out
}
