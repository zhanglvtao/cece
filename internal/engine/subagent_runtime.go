package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type AgentStatus string

const (
	AgentStatusStarting       AgentStatus = "starting"
	AgentStatusRunning        AgentStatus = "running"
	AgentStatusWaitingInput   AgentStatus = "waiting_input"
	AgentStatusWaitingConfirm AgentStatus = "waiting_confirm"
	AgentStatusWaitingPlan    AgentStatus = "waiting_plan"
	AgentStatusCancelling     AgentStatus = "cancelling"
	AgentStatusCancelled      AgentStatus = "cancelled"
	AgentStatusCompleted      AgentStatus = "completed"
	AgentStatusFailed         AgentStatus = "failed"
)

type AgentMessageKind string

const (
	AgentMessageProgress       AgentMessageKind = "progress"
	AgentMessageQuestion       AgentMessageKind = "question"
	AgentMessageConfirmRequest AgentMessageKind = "confirm_request"
	AgentMessageResult         AgentMessageKind = "result"
	AgentMessageError          AgentMessageKind = "error"
)

type AgentMessage struct {
	ID        string           `json:"id"`
	AgentID   string           `json:"agent_id"`
	Kind      AgentMessageKind `json:"kind"`
	Status    AgentStatus      `json:"status"`
	Payload   any              `json:"payload,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
}

type AgentRuntimeSnapshot struct {
	ID                string      `json:"id"`
	Description       string      `json:"description,omitempty"`
	SessionID         string      `json:"session_id,omitempty"`
	Status            AgentStatus `json:"status"`
	Model             string      `json:"model,omitempty"`
	InputTokens       int         `json:"input_tokens,omitempty"`
	OutputTokens      int         `json:"output_tokens,omitempty"`
	CacheReadTokens   int         `json:"cache_read_tokens,omitempty"`
	CacheCreateTokens int         `json:"cache_create_tokens,omitempty"`
	APICalls          int         `json:"api_calls,omitempty"`
	TurnCount         int         `json:"turn_count,omitempty"`
	MaxTurns          int         `json:"max_turns,omitempty"`
	LastActivity      string      `json:"last_activity,omitempty"`
	LastTool          string      `json:"last_tool,omitempty"`
	LastMessage       string      `json:"last_message,omitempty"`
	StartedAt         time.Time   `json:"started_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
	FinishedAt        time.Time   `json:"finished_at,omitempty"`
}

type AgentCommandKind string

type AgentEventPriority string

const (
	AgentCommandSendInput      AgentCommandKind = "send_input"
	AgentCommandAnswerQuestion AgentCommandKind = "answer_question"
	AgentCommandConfirmPending AgentCommandKind = "confirm_pending"
	AgentCommandRejectPending  AgentCommandKind = "reject_pending"
	AgentCommandCancel         AgentCommandKind = "cancel"
	AgentCommandSwitchModel    AgentCommandKind = "switch_model"
)

const (
	AgentEventPriorityBestEffort AgentEventPriority = "best_effort"
	AgentEventPriorityCritical   AgentEventPriority = "critical"
)

type AgentCommand struct {
	Kind    AgentCommandKind
	Input   string
	Answers []protocol.QuestionAnswer
	Model   string
}

type AgentEvent struct {
	Message  AgentMessage
	Priority AgentEventPriority
}

type AgentRuntime struct {
	ID              string
	Description     string
	Engine          *Engine
	Mediator        *EngineMediator
	Context         context.Context
	CancelFunc      context.CancelFunc
	ParentSessionID string
	MaxTurns        int // 0 = unlimited

	mu                sync.Mutex
	Status            AgentStatus
	SessionID         string
	Model             string
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	APICalls          int
	TurnCount         int
	LastActivity      string
	LastTool          string
	LastMessage       string
	lastMessage       AgentMessage
	msgBuf            strings.Builder
	finalResult       string // full final assistant text, captured at TurnCompleted
	StartedAt         time.Time
	UpdatedAt         time.Time
	FinishedAt        time.Time
	Result            tool.AgentSubAgentResult
	updates           chan AgentMessage
	inbox             chan AgentCommand
	outboxCritical    chan AgentEvent
	outboxBestEffort  chan AgentEvent
	cancelOnce        sync.Once
}

func NewAgentRuntime(id, description, model, parentSessionID string, eng *Engine, mediator *EngineMediator, ctx context.Context, cancel context.CancelFunc, maxTurns int) *AgentRuntime {
	now := time.Now()
	return &AgentRuntime{
		ID:              id,
		Description:     description,
		Engine:          eng,
		Mediator:        mediator,
		Context:         ctx,
		CancelFunc:      cancel,
		ParentSessionID: parentSessionID,
		MaxTurns:        maxTurns,
		Status:          AgentStatusStarting,
		Model:           model,
		StartedAt:        now,
		UpdatedAt:        now,
		updates:          make(chan AgentMessage, 32),
		inbox:            make(chan AgentCommand, 32),
		outboxCritical:   make(chan AgentEvent, 32),
		outboxBestEffort: make(chan AgentEvent, 64),
	}
}

func (rt *AgentRuntime) Cancel() {
	rt.cancelOnce.Do(func() {
		rt.mu.Lock()
		rt.Status = AgentStatusCancelling
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		if rt.CancelFunc != nil {
			rt.CancelFunc()
		}
		if rt.Engine != nil {
			rt.Engine.Cancel()
		}
		rt.record(AgentMessage{Kind: AgentMessageError, Status: AgentStatusCancelled, Payload: map[string]any{"cancelled": true}})
	})
}

func (rt *AgentRuntime) StartMailboxLoop() {
	go func() {
		for {
			select {
			case <-rt.Context.Done():
				return
			case cmd := <-rt.inbox:
				rt.handleCommand(cmd)
			}
		}
	}()
}

func (rt *AgentRuntime) EnqueueCommand(ctx context.Context, cmd AgentCommand) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rt.Context.Done():
		return rt.Context.Err()
	case rt.inbox <- cmd:
		return nil
	}
}

func (rt *AgentRuntime) NextEvent(ctx context.Context) (AgentEvent, bool) {
	select {
	case <-ctx.Done():
		return AgentEvent{}, false
	case ev := <-rt.outboxCritical:
		return ev, true
	default:
	}
	select {
	case <-ctx.Done():
		return AgentEvent{}, false
	case ev := <-rt.outboxCritical:
		return ev, true
	case ev := <-rt.outboxBestEffort:
		return ev, true
	}
}

func (rt *AgentRuntime) handleCommand(cmd AgentCommand) {
	switch cmd.Kind {
	case AgentCommandSendInput:
		if strings.TrimSpace(cmd.Input) != "" {
			rt.Engine.QueueInput(cmd.Input)
		}
	case AgentCommandAnswerQuestion:
		rt.Engine.AnswerQuestion(cmd.Answers)
	case AgentCommandConfirmPending:
		rt.Engine.Confirm()
	case AgentCommandRejectPending:
		rt.Engine.RejectToolCalls()
		rt.Engine.RejectPlan()
		rt.Engine.RejectQuestion()
	case AgentCommandCancel:
		rt.Cancel()
	case AgentCommandSwitchModel:
		if rt.Mediator != nil && strings.TrimSpace(cmd.Model) != "" {
			rt.Mediator.Do(protocol.SwitchModelAction{Model: cmd.Model})
		}
	}
}

func (rt *AgentRuntime) Snapshot() AgentRuntimeSnapshot {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return AgentRuntimeSnapshot{
		ID:                rt.ID,
		Description:       rt.Description,
		SessionID:         rt.SessionID,
		Status:            rt.Status,
		Model:             rt.Model,
		InputTokens:       rt.InputTokens,
		OutputTokens:      rt.OutputTokens,
		CacheReadTokens:   rt.CacheReadTokens,
		CacheCreateTokens: rt.CacheCreateTokens,
		APICalls:          rt.APICalls,
		TurnCount:         rt.TurnCount,
		MaxTurns:          rt.MaxTurns,
		LastActivity:      rt.LastActivity,
		LastTool:          rt.LastTool,
		LastMessage:       rt.LastMessage,
		StartedAt:         rt.StartedAt,
		UpdatedAt:         rt.UpdatedAt,
		FinishedAt:        rt.FinishedAt,
	}
}

func (rt *AgentRuntime) LastAgentMessage() AgentMessage {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.lastMessage
}

func (rt *AgentRuntime) WaitInitial(timeout time.Duration) AgentMessage {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		snap := rt.Snapshot()
		if isInterestingAgentStatus(snap.Status) {
			return rt.LastAgentMessage()
		}
		select {
		case msg := <-rt.updates:
			if isInterestingAgentStatus(msg.Status) {
				return msg
			}
		case <-deadline.C:
			snap := rt.Snapshot()
			return AgentMessage{AgentID: rt.ID, Kind: AgentMessageProgress, Status: snap.Status, Payload: snap, CreatedAt: time.Now()}
		}
	}
}

// WaitCompletion blocks until the agent reaches a terminal state (completed,
// failed, or cancelled) or the context is done.
func (rt *AgentRuntime) WaitCompletion(ctx context.Context) AgentMessage {
	for {
		snap := rt.Snapshot()
		if isTerminalAgentStatus(snap.Status) {
			return rt.LastAgentMessage()
		}
		select {
		case msg := <-rt.updates:
			if isTerminalAgentStatus(msg.Status) {
				return msg
			}
		case <-ctx.Done():
			snap := rt.Snapshot()
			return AgentMessage{AgentID: rt.ID, Kind: AgentMessageError, Status: snap.Status, Payload: map[string]any{"cancelled": true}, CreatedAt: time.Now()}
		}
	}
}

func isTerminalAgentStatus(status AgentStatus) bool {
	switch status {
	case AgentStatusCompleted, AgentStatusFailed, AgentStatusCancelled:
		return true
	default:
		return false
	}
}

func isInterestingAgentStatus(status AgentStatus) bool {
	switch status {
	case AgentStatusWaitingInput, AgentStatusWaitingConfirm, AgentStatusWaitingPlan, AgentStatusCompleted, AgentStatusFailed, AgentStatusCancelled:
		return true
	default:
		return false
	}
}

func (rt *AgentRuntime) record(msg AgentMessage) {
	now := time.Now()
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("%s-%d", rt.ID, now.UnixNano())
	}
	if msg.AgentID == "" {
		msg.AgentID = rt.ID
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}

	rt.mu.Lock()
	prevStatus := rt.Status
	if msg.Status != "" {
		rt.Status = msg.Status
	}
	rt.lastMessage = msg
	rt.UpdatedAt = now
	if rt.Status == AgentStatusCompleted || rt.Status == AgentStatusFailed || rt.Status == AgentStatusCancelled {
		rt.FinishedAt = now
	}
	rt.mu.Unlock()

	select {
	case rt.updates <- msg:
	default:
	}

	rt.publishEvent(msg, prevStatus)
}

func (rt *AgentRuntime) publishEvent(msg AgentMessage, prevStatus AgentStatus) {
	ev := AgentEvent{Message: msg, Priority: AgentEventPriorityBestEffort}
	switch msg.Status {
	case AgentStatusWaitingInput, AgentStatusWaitingConfirm, AgentStatusWaitingPlan, AgentStatusCompleted, AgentStatusFailed, AgentStatusCancelled:
		ev.Priority = AgentEventPriorityCritical
	}
	if ev.Priority == AgentEventPriorityCritical {
		select {
		case <-rt.Context.Done():
			return
		case rt.outboxCritical <- ev:
		}
		return
	}
	if prevStatus == msg.Status && msg.Kind == AgentMessageProgress {
		select {
		case rt.outboxBestEffort <- ev:
		default:
		}
		return
	}
	select {
	case rt.outboxBestEffort <- ev:
	default:
	}
}

func (rt *AgentRuntime) handleEvent(ev protocol.Event) (AgentMessage, bool) {
	switch v := ev.(type) {
	case protocol.SessionCreated:
		rt.mu.Lock()
		rt.SessionID = v.ID
		rt.LastActivity = "session " + shortID(v.ID)
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: map[string]any{"session_id": v.ID}}, true
	case protocol.ModelRequestStarted:
		rt.mu.Lock()
		rt.Status = AgentStatusRunning
		rt.TurnCount++
		rt.LastActivity = "thinking"
		rt.msgBuf.Reset()
		rt.LastMessage = ""
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.StreamStarted:
		rt.mu.Lock()
		if v.Model != "" {
			rt.Model = v.Model
		}
		if v.InputTokens > 0 {
			rt.InputTokens = v.InputTokens
		}
		if v.CacheReadTokens > 0 {
			rt.CacheReadTokens = v.CacheReadTokens
		}
		if v.CacheCreationTokens > 0 {
			rt.CacheCreateTokens = v.CacheCreationTokens
		}
		rt.APICalls++
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.AssistantDelta:
		rt.mu.Lock()
		rt.msgBuf.WriteString(v.Text)
		text := strings.TrimSpace(rt.msgBuf.String())
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			text = text[:idx]
		}
		if len(text) > 120 {
			text = text[:117] + "..."
		}
		rt.LastMessage = text
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
			// Update internal snapshot only — do NOT return a message.
			// AssistantDelta fires per token; emitting SubAgentActivityEvent
			// for each one floods the channel. UI gets enough signal from
			// ModelRequestStarted / StreamCompleted / ToolExec* events.
			return AgentMessage{}, false
	case protocol.ToolCallStarted:
		rt.mu.Lock()
		rt.LastTool = "preparing " + v.Name
		rt.LastActivity = rt.LastTool
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.ToolExecStarted:
		rt.mu.Lock()
		rt.LastTool = "running " + v.Name
		rt.LastActivity = rt.LastTool
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.ToolExecCompleted:
		rt.mu.Lock()
		if v.Result.IsError {
			rt.LastTool = v.Name + " failed"
		} else {
			rt.LastTool = v.Name + " done"
		}
		rt.LastActivity = rt.LastTool
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.ToolCallsReady:
		payload := map[string]any{"kind": "tool", "tool_calls": v.Calls}
		return AgentMessage{Kind: AgentMessageConfirmRequest, Status: AgentStatusWaitingConfirm, Payload: payload}, true
	case protocol.ModeChangedEvent:
		rt.mu.Lock()
		rt.LastActivity = "mode: " + string(v.Mode)
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.PlanApprovalRequested:
		payload := map[string]any{"kind": "plan", "plan_file": v.PlanFile, "plan_preview": v.PlanContent}
		return AgentMessage{Kind: AgentMessageConfirmRequest, Status: AgentStatusWaitingPlan, Payload: payload}, true
	case protocol.QuestionAsked:
		payload := map[string]any{"question_id": v.CallID, "questions": v.Questions}
		return AgentMessage{Kind: AgentMessageQuestion, Status: AgentStatusWaitingInput, Payload: payload}, true
	case protocol.StreamCompleted:
		rt.mu.Lock()
		if v.OutputTokens > 0 {
			rt.OutputTokens += v.OutputTokens
		}
		if v.CacheReadTokens > 0 {
			rt.CacheReadTokens = v.CacheReadTokens
		}
		rt.UpdatedAt = time.Now()
		rt.mu.Unlock()
		return AgentMessage{Kind: AgentMessageProgress, Status: AgentStatusRunning, Payload: rt.Snapshot()}, true
	case protocol.RunFailed:
		rt.Result.Err = v.Err
		rt.Result.Content = "sub-agent failed: " + v.Err
		return AgentMessage{Kind: AgentMessageError, Status: AgentStatusFailed, Payload: map[string]any{"error": v.Err}}, true
	case protocol.TurnCompleted:
		rt.mu.Lock()
		rt.finalResult = strings.TrimSpace(rt.msgBuf.String())
		rt.mu.Unlock()
		snap := rt.Snapshot()
		content := snap.LastMessage
		if strings.TrimSpace(content) == "" {
			content = "Sub-agent completed."
		}
		rt.Result = tool.AgentSubAgentResult{
			AgentID:      rt.ID,
			SessionID:    snap.SessionID,
			Status:       string(AgentStatusCompleted),
			Content:      content,
			InputTokens:  v.TotalInputTokens,
			OutputTokens: v.TotalOutputTokens,
			TurnsUsed:    v.TurnCount,
		}
		return AgentMessage{Kind: AgentMessageResult, Status: AgentStatusCompleted, Payload: rt.Result}, true
	}
	return AgentMessage{}, false
}

func (rt *AgentRuntime) resultFromMessage(msg AgentMessage) tool.AgentSubAgentResult {
	snap := rt.Snapshot()
	if msg.Status == AgentStatusCompleted {
		rt.mu.Lock()
		res := rt.Result
		rt.mu.Unlock()
		if res.Content == "" {
			res.Content = snap.LastMessage
		}
		res.AgentID = rt.ID
		res.SessionID = snap.SessionID
		res.Status = string(msg.Status)
		return res
	}
	if msg.Status == AgentStatusFailed || msg.Status == AgentStatusCancelled {
		rt.mu.Lock()
		res := rt.Result
		rt.mu.Unlock()
		res.AgentID = rt.ID
		res.SessionID = snap.SessionID
		res.Status = string(msg.Status)
		if res.Content == "" {
			res.Content = formatAgentMessage(msg)
		}
		if msg.Status == AgentStatusCancelled {
			res.Cancelled = true
		}
		return res
	}
	return tool.AgentSubAgentResult{
		AgentID:   rt.ID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   formatAgentMessage(msg),
	}
}

func formatAgentMessage(msg AgentMessage) string {
	switch msg.Kind {
	case AgentMessageProgress:
		snap, ok := msg.Payload.(AgentRuntimeSnapshot)
		if ok {
			return fmt.Sprintf("Agent %s is %s", msg.AgentID, snap.LastActivity)
		}
		return fmt.Sprintf("Agent %s is running", msg.AgentID)
	case AgentMessageQuestion:
		return formatQuestionPayload(msg)
	case AgentMessageConfirmRequest:
		return formatConfirmPayload(msg)
	case AgentMessageResult:
		if res, ok := msg.Payload.(tool.AgentSubAgentResult); ok {
			return res.Content
		}
	case AgentMessageError:
		if p, ok := msg.Payload.(map[string]any); ok {
			if errStr, ok := p["error"].(string); ok {
				return fmt.Sprintf("Agent %s failed: %s", msg.AgentID, errStr)
			}
		}
	}
	// Fallback to JSON
	b, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Sprintf("agent %s status: %s", msg.AgentID, msg.Status)
	}
	return string(b)
}

func formatQuestionPayload(msg AgentMessage) string {
	p, ok := msg.Payload.(map[string]any)
	if !ok {
		return fmt.Sprintf("Agent %s has a question", msg.AgentID)
	}
	qid, _ := p["question_id"].(string)
	questions, _ := p["questions"].([]protocol.Question)
	var b strings.Builder
	fmt.Fprintf(&b, "Agent %s is waiting for answer (id: %s):\n", msg.AgentID, qid)
	for i, q := range questions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q.Question)
	}
	return b.String()
}

func formatConfirmPayload(msg AgentMessage) string {
	p, ok := msg.Payload.(map[string]any)
	if !ok {
		return fmt.Sprintf("Agent %s is waiting for confirmation", msg.AgentID)
	}
	kind, _ := p["kind"].(string)
	if kind == "plan" {
		planFile, _ := p["plan_file"].(string)
		return fmt.Sprintf("Agent %s is waiting for plan approval: %s", msg.AgentID, planFile)
	}
	calls, _ := p["tool_calls"].([]protocol.ToolUseBlock)
	var names []string
	for _, c := range calls {
		names = append(names, c.Name)
	}
	return fmt.Sprintf("Agent %s is waiting for tool confirmation: %v", msg.AgentID, names)
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func updateSubAgentRelation(ctx context.Context, store session.Store, sessionID, parentID, agentID string) {
	if sessionID == "" || store == nil {
		return
	}
	if rs, ok := store.(session.RelationStore); ok {
		_ = rs.UpdateRelation(ctx, sessionID, parentID, agentID, "agent")
	}
}
