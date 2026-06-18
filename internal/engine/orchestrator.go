package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/tool"
)

type PendingKind string

const (
	PendingNone     PendingKind = ""
	PendingQuestion PendingKind = "question"
	PendingConfirm  PendingKind = "confirm"
	PendingPlan     PendingKind = "plan"
)

type PendingState struct {
	Kind      PendingKind
	RequestID string
	Summary   string
}

type Orchestrator struct {
	mu       sync.Mutex
	factory  SubAgentRuntimeFactory
	store    session.Store
	emit     func(protocol.Event)
	agents   map[string]*AgentRuntime
	archived map[string]*AgentRuntime
	pending  map[string]PendingState
	nextID   int
}

func NewOrchestrator(factory SubAgentRuntimeFactory, store session.Store, emit func(protocol.Event)) *Orchestrator {
	return &Orchestrator{
		factory:  factory,
		store:    store,
		emit:     emit,
		agents:   make(map[string]*AgentRuntime),
		archived: make(map[string]*AgentRuntime),
		pending:  make(map[string]PendingState),
	}
}

func (o *Orchestrator) Run(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig, emitter tool.Emitter) (tool.AgentSubAgentResult, error) {
	switch cfg.Operation {
	case "", "start":
		return o.start(ctx, parent, cfg)
	case "status":
		return o.status(cfg)
	case "send":
		return o.send(cfg)
	case "answer":
		return o.answer(cfg)
	case "confirm":
		return o.confirm(cfg)
	case "reject":
		return o.reject(cfg)
	case "cancel":
		return o.cancel(cfg)
	case "switch_model":
		return o.switchModel(cfg)
	default:
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("unknown operation: %s", cfg.Operation), Err: "unknown_operation"}, nil
	}
}

func (o *Orchestrator) CancelAll(parent *Engine) {
	o.mu.Lock()
	agents := make([]*AgentRuntime, 0, len(o.agents))
	for _, rt := range o.agents {
		agents = append(agents, rt)
	}
	o.mu.Unlock()
	for _, rt := range agents {
		rt.Cancel()
	}
}

func (o *Orchestrator) start(ctx context.Context, parent *Engine, cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	parentSessionID := parent.SessionID()
	parentModel := parent.SessionMetaModel()

	o.mu.Lock()
	o.nextID++
	agentID := fmt.Sprintf("agent-%d", o.nextID)
	o.mu.Unlock()

	rt, err := o.factory.NewSubAgentRuntime(ctx, SubAgentBuildConfig{
		AgentID:           agentID,
		Description:       cfg.Description,
		Model:             cfg.Model,
		ParentSessionID:   parentSessionID,
		SystemPromptExtra: cfg.SystemPromptExtra,
		Tools:             cfg.Tools,
		MaxTurns:          cfg.MaxTurns,
	})
	if err != nil {
		slog.Error("orchestrator: worker build failed", "agent_id", agentID, "error", err)
		if o.emit != nil {
			o.emit(protocol.SubAgentFailedEvent{ID: agentID, Description: cfg.Description, ParentSessionID: parentSessionID, Error: err.Error()})
		}
		return tool.AgentSubAgentResult{AgentID: agentID, Status: string(AgentStatusFailed), Content: fmt.Sprintf("worker build failed: %v", err), Err: err.Error()}, err
	}

	if cfg.Model == "" {
		rt.mu.Lock()
		rt.Model = parentModel
		rt.mu.Unlock()
	}
	rt.Engine.SetEffort("low")

	o.mu.Lock()
	o.agents[agentID] = rt
	o.mu.Unlock()

	if o.emit != nil {
		o.emit(protocol.SubAgentStartedEvent{ID: agentID, Description: cfg.Description, ParentSessionID: parentSessionID})
	}
	go o.bridgeRuntime(parent, rt, parentSessionID)

	if err := rt.Engine.Input(rt.Context, cfg.Prompt); err != nil {
		rt.Cancel()
		return tool.AgentSubAgentResult{AgentID: agentID, Status: string(AgentStatusFailed), Content: fmt.Sprintf("worker start failed: %v", err), Err: err.Error()}, err
	}

	snap := rt.Snapshot()
	slog.Info("orchestrator: worker started",
		"agent_id", agentID,
		"profile", "worker",
		"parent_session_id", parentSessionID,
		"status", snap.Status,
	)
	return tool.AgentSubAgentResult{
		AgentID:   agentID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   fmt.Sprintf("Agent %s started asynchronously. Use Agent status to poll progress.", agentID),
	}, nil
}

func (o *Orchestrator) status(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	snap := rt.Snapshot()
	msg := rt.LastAgentMessage()
	if msg.Kind == "" {
		msg = AgentMessage{AgentID: rt.ID, Kind: AgentMessageProgress, Status: snap.Status, Payload: snap}
	}
	return tool.AgentSubAgentResult{
		AgentID:   rt.ID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   formatAgentMessage(msg),
	}, nil
}

func (o *Orchestrator) send(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	if cfg.Input == "" {
		return tool.AgentSubAgentResult{Content: "input is required for send operation", Err: "missing_input"}, nil
	}
	rt.Engine.QueueInput(cfg.Input)
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("input queued for agent %s", rt.ID)}, nil
}

func (o *Orchestrator) answer(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	answers := make([]protocol.QuestionAnswer, len(cfg.Answers))
	for i, a := range cfg.Answers {
		answers[i] = protocol.QuestionAnswer{Question: a.Question, Selected: a.Selected, Custom: a.Custom}
	}
	rt.Engine.AnswerQuestion(answers)
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("answers submitted to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) confirm(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	rt.Engine.Confirm()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("confirmation sent to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) reject(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	rt.Engine.RejectToolCalls()
	rt.Engine.RejectPlan()
	rt.Engine.RejectQuestion()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("rejection sent to agent %s", rt.ID)}, nil
}

func (o *Orchestrator) cancel(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	rt.Cancel()
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("agent %s cancelled", rt.ID), Cancelled: true}, nil
}

func (o *Orchestrator) switchModel(cfg tool.AgentSubAgentConfig) (tool.AgentSubAgentResult, error) {
	rt := o.get(cfg.AgentID)
	if rt == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s not found", cfg.AgentID), Err: "agent_not_found"}, nil
	}
	if res, done := finishedAgentResult(rt); done {
		return res, nil
	}
	if cfg.Model == "" {
		return tool.AgentSubAgentResult{Content: "model is required for switch_model operation", Err: "missing_model"}, nil
	}
	if rt.Mediator == nil {
		return tool.AgentSubAgentResult{Content: fmt.Sprintf("agent %s has no mediator (switch_model not available)", rt.ID), Err: "no_mediator"}, nil
	}
	rt.Mediator.Do(protocol.SwitchModelAction{Model: cfg.Model})
	snap := rt.Snapshot()
	return tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(snap.Status), Content: fmt.Sprintf("agent %s switched to model %s", rt.ID, cfg.Model)}, nil
}

func (o *Orchestrator) bridgeRuntime(parent *Engine, rt *AgentRuntime, parentSessionID string) {
	for {
		select {
		case <-rt.Context.Done():
			msg := AgentMessage{AgentID: rt.ID, Kind: AgentMessageError, Status: AgentStatusCancelled, Payload: map[string]any{"cancelled": true}}
			rt.record(msg)
			o.handleTerminalMessage(parent, rt, parentSessionID, msg)
			return
		case ev := <-rt.Engine.Events():
			if ev == nil {
				continue
			}
			msg, handled := rt.handleEvent(ev)
			if !handled {
				continue
			}
			rt.record(msg)

			pending := PendingState{}
			switch msg.Status {
			case AgentStatusWaitingInput:
				pending.Kind = PendingQuestion
				pending.Summary = "waiting for answer"
			case AgentStatusWaitingConfirm:
				pending.Kind = PendingConfirm
				pending.Summary = "waiting for confirmation"
			case AgentStatusWaitingPlan:
				pending.Kind = PendingPlan
				pending.Summary = "waiting for plan approval"
			}
			o.mu.Lock()
			if pending.Kind == PendingNone {
				delete(o.pending, rt.ID)
			} else {
				o.pending[rt.ID] = pending
			}
			o.mu.Unlock()

			if _, ok := ev.(protocol.SessionCreated); ok {
				updateSubAgentRelation(context.Background(), o.store, rt.SessionID, parentSessionID, rt.ID)
			}

			snap := rt.Snapshot()
			activity := snap.LastActivity
			if activity == "" {
				switch msg.Status {
				case AgentStatusWaitingInput:
					activity = "waiting for answer"
				case AgentStatusWaitingConfirm:
					activity = "waiting for confirmation"
				case AgentStatusWaitingPlan:
					activity = "waiting for plan approval"
				case AgentStatusCompleted:
					activity = "completed"
				case AgentStatusCancelled:
					activity = "cancelled"
				case AgentStatusFailed:
					activity = "failed"
				default:
					activity = "running"
				}
			}
			if o.emit != nil {
				o.emit(protocol.SubAgentActivityEvent{
					ID:               rt.ID,
					SessionID:        snap.SessionID,
					ParentSessionID:  parentSessionID,
					Activity:         activity,
					Status:           string(snap.Status),
					Model:            snap.Model,
					InputTokens:      snap.InputTokens,
					OutputTokens:     snap.OutputTokens,
					CacheReadTokens:  snap.CacheReadTokens,
					TurnCount:        snap.TurnCount,
					ToolCall:         snap.LastTool,
					LastAssistantMsg: snap.LastMessage,
				})
			}
			slog.Info("orchestrator: state transition",
				"agent_id", rt.ID,
				"parent_session_id", parentSessionID,
				"status", snap.Status,
				"activity", activity,
			)

			if rt.MaxTurns > 0 && rt.TurnCount >= rt.MaxTurns {
				rt.mu.Lock()
				rt.finalResult = strings.TrimSpace(rt.msgBuf.String())
				rt.mu.Unlock()
				rt.Result = tool.AgentSubAgentResult{AgentID: rt.ID, SessionID: snap.SessionID, Status: string(AgentStatusCompleted), Content: snap.LastMessage, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount, HitMaxTurns: true}
				if o.emit != nil {
					o.emit(protocol.SubAgentCompletedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount, HitMaxTurns: true})
				}
				parent.accumulateSubAgentTokens(rt.Result)
				rt.Engine.Cancel()
				return
			}

			switch msg.Status {
			case AgentStatusCompleted:
				o.handleTerminalMessage(parent, rt, parentSessionID, msg)
				return
			case AgentStatusFailed, AgentStatusCancelled:
				o.handleTerminalMessage(parent, rt, parentSessionID, msg)
				return
			}
		}
	}
}

func (o *Orchestrator) handleTerminalMessage(parent *Engine, rt *AgentRuntime, parentSessionID string, msg AgentMessage) {
	snap := rt.Snapshot()
	switch msg.Status {
	case AgentStatusCompleted:
		result := parent.writeSubAgentArtifact(rt.resultFromMessage(msg), rt)
		parent.accumulateSubAgentTokens(result)
		if o.emit != nil {
			o.emit(protocol.SubAgentCompletedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, InputTokens: snap.InputTokens, OutputTokens: snap.OutputTokens, TurnsUsed: snap.TurnCount, HitMaxTurns: result.HitMaxTurns})
		}
	case AgentStatusFailed, AgentStatusCancelled:
		errText := ""
		if msg.Status == AgentStatusCancelled {
			errText = "cancelled"
		} else if p, ok := msg.Payload.(map[string]any); ok {
			if s, ok := p["error"].(string); ok {
				errText = s
			}
		}
		if o.emit != nil {
			o.emit(protocol.SubAgentFailedEvent{ID: rt.ID, Description: rt.Description, SessionID: snap.SessionID, ParentSessionID: parentSessionID, Error: errText})
		}
	}
}

func (o *Orchestrator) get(agentID string) *AgentRuntime {
	o.mu.Lock()
	defer o.mu.Unlock()
	if rt := o.agents[agentID]; rt != nil {
		return rt
	}
	return o.archived[agentID]
}

func (o *Orchestrator) finish(agentID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if rt := o.agents[agentID]; rt != nil {
		o.archived[agentID] = rt
		delete(o.agents, agentID)
	}
	delete(o.pending, agentID)
}

func finishedAgentResult(rt *AgentRuntime) (tool.AgentSubAgentResult, bool) {
	snap := rt.Snapshot()
	if !isTerminalAgentStatus(snap.Status) {
		return tool.AgentSubAgentResult{}, false
	}
	return tool.AgentSubAgentResult{
		AgentID:   rt.ID,
		SessionID: snap.SessionID,
		Status:    string(snap.Status),
		Content:   fmt.Sprintf("agent %s has finished", rt.ID),
		Err:       "agent_finished",
	}, true
}
