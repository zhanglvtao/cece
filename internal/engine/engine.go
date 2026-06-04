package engine

import (
	"context"
	"errors"
	"fmt"
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

// Engine is the core agent engine. It manages conversation state, dispatches
// user input to the agent loop, and emits protocol.Events on a channel.
//
// Engine implements agent.TurnEngine (for TurnBootstrap) and satisfies the
// ui.Sender / ui.Actor / ui.Eventer interfaces consumed by the BubbleTea UI.
type Engine struct {
	mu               sync.Mutex
	client           agent.ModelClient
	registry         *tool.Registry
	assembler        *prompt.ContextAssembler
	projectDir       string
	planState        *tool.PlanModeState
	taskList         *tool.TaskList
	history          []agent.Message
	historyWatermark int // history length before current turn; used to rollback on interrupt
	cancel           context.CancelFunc
	confirmCh        chan struct{} // set per Input call, cleared on completion
	yolo             bool          // auto-approve tool execution without UI confirmation
	maxTokens        int           // configurable max output tokens

	ContextWindowFor    func(model string) int               // returns context window for a model ID
	ModelClientFor      func(model string) agent.ModelClient // returns ModelClient for a model ID, nil = use current client
	store               session.Store                        // optional persistence backend
	sessionID           string                               // current session ID, empty = not yet created
	sessionCreated      bool                                 // true after first Input creates a session
	modelName           string                               // current model name for meta persistence
	contextWindow       int                                  // current context window size for meta persistence
	protocol            string                               // current protocol (anthropic, aiden, codebase, etc.)
	configName          string                               // current provider config name
	lastInputTokens     int                                  // last request input tokens for resume water level
	totalInputTokens    int                                  // cumulative input tokens across turns
	totalOutputTokens   int                                  // cumulative output tokens across turns
	apiCalls            int                                  // cumulative API call count
	toolCounts          map[string]int                       // cumulative tool execution counts
	turnCount           int                                  // cumulative conversation turn count
	cacheReadTokens     int                                  // cumulative cache read tokens
	cacheCreationTokens int                                  // cumulative cache creation tokens
	lastCompactTurn     int                                  // turn count at last compact/prune
	lastNudgeTurn       int                                  // turn count at last nudge injection
	inputQueue          *userInputQueue                      // queued user inputs while agent is busy
	nextSubAgentID      int                                  // monotonic ID for sub-agent lifecycle events
	questionAnswers     []tool.QuestionAnswer
	eventCh             chan protocol.Event // global event channel for async responses
}

func NewEngine(client agent.ModelClient, registry *tool.Registry, yolo bool, maxTokens int, assembler *prompt.ContextAssembler, projectDir string) *Engine {
	return &Engine{
		client:     client,
		registry:   registry,
		assembler:  assembler,
		projectDir: projectDir,
		planState:  tool.NewPlanModeState(),
		taskList:   tool.NewTaskList(),
		yolo:       yolo,
		maxTokens:  maxTokens,
		inputQueue: &userInputQueue{},
		toolCounts: make(map[string]int),
		eventCh:    make(chan protocol.Event, 4096),
	}
}

// ── TurnEngine interface implementation ───────────────────────────────────

func (e *Engine) ProjectDir() string                  { return e.projectDir }
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
func (e *Engine) Yolo() bool                               { return e.yolo }
func (e *Engine) MaxTokens() int                           { return e.maxTokens }
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

	splitIdx := boundaries[keepTurn]
	summarize := snapshot[:splitIdx]
	keep := snapshot[splitIdx:]

	if len(summarize) == 0 {
		return "", 0, 0, fmt.Errorf("no messages to summarize before turn %d", keepTurn)
	}

	compactor := agent.NewCompactor(client, 0)
	summary, err := compactor.GenerateSummary(ctx, summarize)
	if err != nil {
		return "", 0, 0, err
	}

	tokensBefore := agent.EstimateMessagesTokens(snapshot)

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
	e.lastNudgeTurn = e.lastCompactTurn
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

	boundaries := agent.TurnBoundaries(history)
	totalTurns := len(boundaries)

	if toTurn > totalTurns {
		toTurn = totalTurns
	}
	if fromTurn >= toTurn {
		return 0, 0, 0
	}

	truncatedCount, tokensBefore, tokensAfter := agent.TrimToolResultsInRange(history, fromTurn, toTurn)

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
	tokensBefore := agent.EstimateMessagesTokens(snapshot)

	if turn <= 0 || turn > len(boundaries) {
		return tokensBefore, tokensBefore
	}

	startIdx := boundaries[turn-1]

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
	tokensAfter := agent.EstimateMessagesTokens(append([]agent.Message{boundary}, snapshot[startIdx:]...))

	e.mu.Lock()
	e.history = newHistory
	sessionID := e.sessionID
	e.lastCompactTurn = len(agent.TurnBoundaries(newHistory))
	e.lastNudgeTurn = e.lastCompactTurn
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
		RunSubAgent: e.runSubAgent,
	}
}

func (e *Engine) runSubAgent(ctx context.Context, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
	e.mu.Lock()
	client := e.client
	projectDir := e.projectDir
	maxTokens := e.maxTokens
	modelClientFor := e.ModelClientFor
	e.mu.Unlock()

	// If a different model is requested, route to the corresponding client.
	if cfg.Model != "" && modelClientFor != nil {
		if subClient := modelClientFor(cfg.Model); subClient != nil {
			client = subClient
		}
	}

	// Build tool registry for the sub-agent.
	// Default: all tools except Agent (prevent nesting).
	// If LLM specifies tools, use that list (still excluding Agent).
	subRegistry := tool.NewRegistry()
	agentExcluded := map[string]struct{}{"Agent": {}}

	if len(cfg.Tools) > 0 {
		// LLM-specified tool list
		for _, name := range cfg.Tools {
			if _, skip := agentExcluded[name]; skip {
				continue
			}
			t, ok := e.registry.Get(name)
			if !ok {
				continue
			}
			subRegistry.Register(t)
		}
	} else {
		// Default: all parent tools except Agent
		for _, def := range e.registry.Definitions() {
			if _, skip := agentExcluded[def.Name]; skip {
				continue
			}
			t, ok := e.registry.Get(def.Name)
			if !ok {
				continue
			}
			subRegistry.Register(t)
		}
	}

	// Allocate lifecycle ID before building sub-agent config so internal activity can be routed.
	e.mu.Lock()
	e.nextSubAgentID++
	agentID := fmt.Sprintf("agent-%d", e.nextSubAgentID)
	e.mu.Unlock()
	activityCh := make(chan agent.Event, 64)
	go e.forwardSubAgentActivity(agentID, activityCh)

	subAgentConfig := agent.SubAgentConfig{
		Prompt:            cfg.Prompt,
		Description:       cfg.Description,
		SystemPromptExtra: cfg.SystemPromptExtra,
		ProjectDir:        projectDir,
		MaxTokens:         maxTokens,
		MaxTurns:          cfg.MaxTurns,
		Events:            activityCh,
	}

	subAgent := agent.NewSubAgent(client, subRegistry, subAgentConfig)

	// Emit start event
	e.emitEvent(protocol.SubAgentStartedEvent{
		ID:          agentID,
		Description: cfg.Description,
	})

	result := subAgent.Run(ctx)
	close(activityCh)

	if result.Cancelled || result.Err != "" {
		errText := result.Err
		if errText == "" {
			errText = result.Content
		}
		e.emitEvent(protocol.SubAgentFailedEvent{
			ID:          agentID,
			Description: cfg.Description,
			Error:       errText,
		})
	} else {
		e.emitEvent(protocol.SubAgentCompletedEvent{
			ID:           agentID,
			Description:  cfg.Description,
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
			TurnsUsed:    result.TurnsUsed,
			HitMaxTurns:  result.HitMaxTurns,
		})
	}

	// Accumulate tokens to parent session
	e.mu.Lock()
	e.totalInputTokens += result.InputTokens
	e.totalOutputTokens += result.OutputTokens
	e.apiCalls += result.TurnsUsed + 1
	e.mu.Unlock()

	return tool.AgentSubAgentResult{
		Content:      result.Content,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		TurnsUsed:    result.TurnsUsed,
		HitMaxTurns:  result.HitMaxTurns,
		Cancelled:    result.Cancelled,
		Err:          result.Err,
	}, nil
}

func (e *Engine) forwardSubAgentActivity(agentID string, ch <-chan agent.Event) {
	toolLabels := map[string]string{}
	for ev := range ch {
		activity := subAgentActivityText(ev, toolLabels)
		if activity == "" {
			continue
		}
		e.emitEvent(protocol.SubAgentActivityEvent{ID: agentID, Activity: activity})
	}
}

func subAgentActivityText(ev agent.Event, toolLabels map[string]string) string {
	switch v := ev.(type) {
	case agent.ModelRequestStarted:
		if v.Reason == "tool_result" && len(v.ToolResults) > 0 {
			return "thinking after " + strings.Join(v.ToolResults, ", ")
		}
		return "thinking"
	case agent.ToolCallStarted:
		return "preparing " + v.Name
	case agent.ToolCallCompleted:
		label := toolActivityLabel(v.Name, v.Input)
		toolLabels[v.ID] = label
		return label
	case agent.ToolExecStarted:
		if label := toolLabels[v.ID]; label != "" {
			return label
		}
		return v.Name
	case agent.ToolExecDelta:
		text := strings.TrimSpace(v.Text)
		if text == "" {
			return "running tool"
		}
		lines := strings.Split(text, "\n")
		return lines[len(lines)-1]
	case agent.ToolExecCompleted:
		if v.Result.IsError {
			return v.Name + " failed"
		}
		return v.Name + " done"
	case agent.AssistantDelta:
		text := strings.TrimSpace(v.Text)
		if text != "" {
			return "writing: " + text
		}
	}
	return ""
}

func toolActivityLabel(name string, input []byte) string {
	s := strings.TrimSpace(string(input))
	if s == "" || s == "{}" {
		return name
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return name + " " + s
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

// ── Context Nudge ─────────────────────────────────────────────────────────

const (
	nudgeTurnThreshold       = 20 // turns since last compact before first nudge
	nudgeTurnInterval        = 10 // turns between subsequent nudges
	nudgeContextPctThreshold = 60 // minimum context % used to trigger nudge
)

// shouldNudge checks whether a context-pressure nudge should be injected.
// Returns (shouldNudge, turnsSinceCompact, contextPct, contextWindow).
func (e *Engine) shouldNudge() (bool, int, int, int) {
	e.mu.Lock()
	history := e.history
	lastCompactTurn := e.lastCompactTurn
	lastNudgeTurn := e.lastNudgeTurn
	lastInputTokens := e.lastInputTokens
	contextWindow := e.contextWindow
	e.mu.Unlock()

	totalTurns := len(agent.TurnBoundaries(history))
	turnsSinceCompact := totalTurns - lastCompactTurn
	turnsSinceNudge := totalTurns - lastNudgeTurn
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

	// Both conditions must be met: enough turns since compact AND high context usage.
	if turnsSinceCompact < nudgeTurnThreshold {
		slog.Debug("nudge check: not enough turns since compact",
			"turnsSinceCompact", turnsSinceCompact,
			"threshold", nudgeTurnThreshold,
			"contextPct", contextPct,
			"lastInputTokens", lastInputTokens,
			"contextWindow", contextWindow,
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
	// Rate-limit: must accumulate enough turns since last nudge.
	if turnsSinceNudge < nudgeTurnInterval {
		slog.Debug("nudge check: rate limited",
			"turnsSinceNudge", turnsSinceNudge,
			"interval", nudgeTurnInterval,
			"contextPct", contextPct,
			"turnsSinceCompact", turnsSinceCompact,
		)
		return false, turnsSinceCompact, contextPct, contextWindow
	}

	slog.Info("nudge triggered",
		"contextPct", contextPct,
		"turnsSinceCompact", turnsSinceCompact,
		"turnsSinceNudge", turnsSinceNudge,
		"lastInputTokens", lastInputTokens,
		"contextWindow", contextWindow,
		"totalTurns", totalTurns,
	)
	return true, turnsSinceCompact, contextPct, contextWindow
}

// injectNudge appends a context-pressure system-reminder to the snapshot
// and updates nudge tracking state. Returns the modified snapshot.
func (e *Engine) injectNudge(snapshot []agent.Message, turnsSinceCompact, contextPct, contextWindow int) []agent.Message {
	usedK := (e.lastInputTokens + 999) / 1000
	windowK := (contextWindow + 999) / 1000
	nudgeText := fmt.Sprintf(
		"<system-reminder>\nContext pressure: %d%% used (%dK/%dK), %d turns since last compact.\nConsider using Compact, TrimToolResults, or Prune to free context.\n</system-reminder>",
		contextPct, usedK, windowK, turnsSinceCompact,
	)
	slog.Info("injecting context nudge into snapshot", "contextPct", contextPct, "usedK", usedK, "windowK", windowK, "turnsSinceCompact", turnsSinceCompact)
	snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: nudgeText})

	e.mu.Lock()
	e.lastNudgeTurn = len(agent.TurnBoundaries(e.history))
	e.mu.Unlock()

	return snapshot
}

// IncrementAPICalls increments the API call counter.
func (e *Engine) IncrementAPICalls() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.apiCalls++
}

// DrainNudgeReminder checks if a context-pressure nudge should be injected
// and returns the nudge text (or empty string if no nudge needed).
// Called by the agentic loop after each LLM response.
func (e *Engine) DrainNudgeReminder() string {
	ok, turns, pct, cw := e.shouldNudge()
	if !ok {
		return ""
	}
	usedK := (e.lastInputTokens + 999) / 1000
	windowK := (cw + 999) / 1000
	nudge := fmt.Sprintf(
		"<system-reminder>\nContext pressure: %d%% used (%dK/%dK), %d turns since last compact.\nConsider using Compact, TrimToolResults, or Prune to free context.\n</system-reminder>",
		pct, usedK, windowK, turns,
	)
	// Update nudge tracking state.
	e.mu.Lock()
	e.lastNudgeTurn = len(agent.TurnBoundaries(e.history))
	e.mu.Unlock()

	slog.Info("injecting context nudge in agentic loop", "contextPct", pct, "usedK", usedK, "windowK", windowK, "turnsSinceCompact", turns)
	return nudge
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

	compactor := agent.NewCompactor(client, 0)
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

	// Record watermark before appending user message so we can rollback on interrupt.
	e.mu.Lock()
	e.historyWatermark = len(e.history)
	e.mu.Unlock()

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

	e.mu.Lock()
	e.cancel = cancel
	e.confirmCh = confirmCh
	e.mu.Unlock()

	go func() {
		defer close(events)
		defer func() {
			e.mu.Lock()
			// Rollback history if the turn was interrupted (context cancelled).
			if ctx.Err() != nil {
				if e.historyWatermark < len(e.history) {
					slog.Info("turn interrupted, rolling back history", "watermark", e.historyWatermark, "current", len(e.history))
					e.history = e.history[:e.historyWatermark]
				}
			}
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
	bootstrap := agent.NewTurnBootstrap(e, agent.NewSessionCoordinator(e.store), nil)
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
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildFullPlanReminder(plansDir)})
			if consumePlanReminder {
				planState.SetReminderType("sparse")
			}
		case "sparse":
			snapshot = append(snapshot, agent.Message{Role: agent.UserRole, Content: tool.BuildSparsePlanReminder(plansDir)})
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
func (e *Engine) SetNudgeStateForTest(lastCompactTurn, lastNudgeTurn, lastInputTokens int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastCompactTurn = lastCompactTurn
	e.lastNudgeTurn = lastNudgeTurn
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
