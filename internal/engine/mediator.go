package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"cece/internal/chat"
	"cece/internal/protocol"
	"cece/internal/session"
	"cece/internal/tool"
)

// EngineMediator wraps Engine and handles B-class UI orchestration commands
// (SwitchModel, LoadSession, ListModels, CycleMode) that don't belong in the
// core agent engine. It embeds *Engine to satisfy ui.Sender / ui.Actor / ui.Eventer.
type EngineMediator struct {
	*Engine
	store            session.Store
	providerResolver func(configName string) (apiKey, baseURL, authMode, authHelper, protocol string)
	createClientFn   func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) chat.ModelClient
	listAllModelsFn  func(ctx context.Context) ([]protocol.ModelInfo, error)
}

func NewEngineMediator(
	eng *Engine,
	store session.Store,
	providerResolver func(string) (string, string, string, string, string),
	createClientFn func(string, string, string, string, string, string, string) chat.ModelClient,
	listAllModelsFn func(context.Context) ([]protocol.ModelInfo, error),
) *EngineMediator {
	return &EngineMediator{
		Engine:           eng,
		store:            store,
		providerResolver: providerResolver,
		createClientFn:   createClientFn,
		listAllModelsFn:  listAllModelsFn,
	}
}

// Do dispatches A-class actions to Engine and handles B-class actions here.
func (m *EngineMediator) Do(action protocol.Action) {
	switch a := action.(type) {
	// A-class — delegate to Engine
	case protocol.ConfirmAction:
		m.Engine.Confirm()
	case protocol.CancelAction:
		m.Engine.Cancel()
	case protocol.ApprovePlanAction:
		m.Engine.ApprovePlan()
	case protocol.RejectPlanAction:
		m.Engine.RejectPlan()
	case protocol.AnswerQuestionAction:
		m.Engine.AnswerQuestion(a.Answers)
	case protocol.QueueInputAction:
		m.Engine.QueueInput(a.Text)
	case protocol.ClearHistoryAction:
		m.Engine.ClearHistory()
	case protocol.CompactAction:
		go m.Engine.CompactHistory(context.Background())

	// B-class — mediator handles
	case protocol.SwitchModelAction:
		m.switchModel(a)
	case protocol.LoadSessionAction:
		go m.loadSession(a.SessionID)
	case protocol.ListModelsAction:
		go m.listModels()
	case protocol.CyclePermissionModeAction:
		go m.cycleMode()
	case protocol.SetPermissionModeAction:
		m.setMode(a.Mode)
	}
}

// ── B-class command implementations ────────────────────────────────────────

func (m *EngineMediator) switchModel(a protocol.SwitchModelAction) {
	client := m.createClientFn(a.Protocol, a.APIKey, a.Model, a.BaseURL, a.AuthMode, a.AuthHelper, a.ConfigName)
	maxCw := a.MaxContextWindow
	if maxCw <= 0 {
		if m.ContextWindowFor != nil {
			maxCw = m.ContextWindowFor(a.Model)
		}
		if maxCw <= 0 {
			maxCw = 200000
		}
	}
	m.Engine.SetClient(client)
	m.Engine.Assembler().SetMaxContextTokens(maxCw)
	m.Engine.SetModelInfo(a.Model, maxCw)
	m.Engine.ResetModelInfo(a.Model, maxCw, a.Protocol, a.ConfigName)
	slog.Info("model switched", "model", a.Model, "max_context", maxCw)
}

func (m *EngineMediator) loadSession(sessionID string) {
	rawMsgs, err := m.store.LoadMessages(context.Background(), sessionID)
	if err != nil {
		m.Engine.EmitEvent(protocol.SessionLoadedEvent{Err: err.Error()})
		return
	}
	var msgs []chat.Message
	for _, raw := range rawMsgs {
		var msg chat.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Warn("skipping corrupt message in session", "session", sessionID, "error", err)
			continue
		}
		msgs = append(msgs, msg)
	}
	m.Engine.LoadHistory(context.Background(), sessionID, msgs)

	// Restore meta from session store
	sess, err := m.store.Get(context.Background(), sessionID)
	if err == nil {
		m.Engine.SetTokenState(sess.LastInputTokens, sess.TotalInputTokens, sess.TotalOutputTokens)

		model := sess.Model
		cw := sess.ContextWindow
		proto := sess.Protocol
		cn := sess.ConfigName

		if model != "" && m.providerResolver != nil {
			apiKey, baseURL, authMode, authHelper, resolvedProto := m.providerResolver(cn)
			proto = resolvedProto
			client := m.createClientFn(proto, apiKey, model, baseURL, authMode, authHelper, cn)
			m.Engine.SetClient(client)
		}
		m.Engine.ResetModelInfo(model, cw, proto, cn)
		if cw > 0 {
			m.Engine.Assembler().SetMaxContextTokens(cw)
		}
	}

	model, cw, lastInput, inTok, outTok, proto, cfgName := m.Engine.SessionMeta()
	m.Engine.EmitEvent(protocol.SessionLoadedEvent{
		SessionID:     sessionID,
		History:       m.Engine.History(),
		Model:         model,
		ContextWindow: cw,
		LastInput:     lastInput,
		TotalInput:    inTok,
		TotalOutput:   outTok,
		Protocol:      proto,
		ConfigName:    cfgName,
	})
}

func (m *EngineMediator) listModels() {
	if m.listAllModelsFn == nil {
		m.Engine.EmitEvent(protocol.ModelsLoadedEvent{Err: "multi-provider listing not configured"})
		return
	}
	models, err := m.listAllModelsFn(context.Background())
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	m.Engine.EmitEvent(protocol.ModelsLoadedEvent{Models: models, Err: errMsg})
}

func (m *EngineMediator) cycleMode() {
	ps := m.Engine.PlanModeState()
	if ps == nil {
		ps = tool.NewPlanModeState()
		m.Engine.SetPlanModeState(ps)
	}
	nextMode := ps.CycleMode()
	m.emitModeChanged(nextMode)
}

func (m *EngineMediator) setMode(mode protocol.PermissionMode) {
	ps := m.Engine.PlanModeState()
	if ps == nil {
		ps = tool.NewPlanModeState()
		m.Engine.SetPlanModeState(ps)
	}
	nextMode := ps.SetMode(tool.PermissionMode(mode))
	m.emitModeChanged(nextMode)
}

func (m *EngineMediator) emitModeChanged(mode tool.PermissionMode) {
	var displayText string
	switch mode {
	case tool.PermissionModeAutoAccept:
		displayText = "Auto-accept mode"
	case tool.PermissionModePlan:
		displayText = "Entered plan mode"
	default:
		displayText = "Default mode"
	}
	m.Engine.EmitEvent(protocol.ModeChangedEvent{Mode: protocol.PermissionMode(mode), Message: displayText})
}

// parseAuthMode converts a string auth mode to an int matching claude.AuthMode values.
func parseAuthMode(s string) int {
	switch strings.ToLower(s) {
	case "bearer":
		return 1
	default:
		return 0
	}
}

// Ensure unused imports are handled cleanly.
var _ = errors.New
