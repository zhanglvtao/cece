package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"cece/internal/agent"
	"cece/internal/mcp"
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
	createClientFn   func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient
	listAllModelsFn  func(ctx context.Context) ([]protocol.ModelInfo, error)
	mcpManager       *mcp.Manager
}

func NewEngineMediator(
	eng *Engine,
	store session.Store,
	providerResolver func(string) (string, string, string, string, string),
	createClientFn func(string, string, string, string, string, string, string) agent.ModelClient,
	listAllModelsFn func(context.Context) ([]protocol.ModelInfo, error),
	mcpManager *mcp.Manager,
) *EngineMediator {
	return &EngineMediator{
		Engine:           eng,
		store:            store,
		providerResolver: providerResolver,
		createClientFn:   createClientFn,
		listAllModelsFn:  listAllModelsFn,
		mcpManager:       mcpManager,
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
	case protocol.TruncateToolResultsAction:
		m.Engine.TruncateToolResults()

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
	case protocol.SetExitTargetModeAction:
		if ps := m.Engine.PlanModeState(); ps != nil {
			ps.SetExitTargetMode(tool.PermissionMode(a.Mode))
		}
	case protocol.RenameSessionAction:
		go m.renameSession(a.SessionID, a.Title)
	case protocol.AutoTitleSessionAction:
		go m.autoTitleSession(a.SessionID)
	case protocol.ListMCPAction:
		go m.listMCPServers()
	case protocol.ConnectMCPAction:
		go m.connectMCP(a.Name)
	case protocol.DisconnectMCPAction:
		go m.disconnectMCP(a.Name)
	case protocol.ListToolsAction:
		go m.listTools()
	case protocol.DryRunRequestAction:
		m.Engine.DryRunRequest(a.Input)
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
	var msgs []agent.Message
	for _, raw := range rawMsgs {
		var msg agent.Message
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
		m.Engine.SetStatusBarState(sess.StatusBar)

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
	sb := m.Engine.StatusBarSnapshot()
	m.Engine.EmitEvent(protocol.SessionLoadedEvent{
		SessionID:           sessionID,
		History:             m.Engine.History(),
		Model:               model,
		ContextWindow:       cw,
		LastInput:           lastInput,
		TotalInput:          inTok,
		TotalOutput:         outTok,
		Protocol:            proto,
		ConfigName:          cfgName,
		APICalls:            sb.APICalls,
		ToolCounts:          sb.ToolCounts,
		CacheReadTokens:     sb.CacheReadTokens,
		CacheCreationTokens: sb.CacheCreationTokens,
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

func (m *EngineMediator) renameSession(sessionID, title string) {
	if m.store == nil || sessionID == "" || title == "" {
		return
	}
	if err := m.store.Rename(context.Background(), sessionID, title); err != nil {
		slog.Error("rename session", "sessionID", sessionID, "error", err)
	}
}

// autoTitleSession generates a session title using a lightweight model call
// and renames the session. Best-effort; failures are logged and ignored.
func (m *EngineMediator) autoTitleSession(sessionID string) {
	if m.store == nil || sessionID == "" {
		return
	}

	ctx := context.Background()

	// Load messages to build a summary for the title generation prompt.
	msgs, err := m.store.LoadMessages(ctx, sessionID)
	if err != nil || len(msgs) == 0 {
		return
	}

	// Extract up to 6 recent user/assistant text messages for context.
	var conversationLines []string
	for _, raw := range msgs {
		var partial struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &partial) != nil {
			continue
		}
		if partial.Role != "user" && partial.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(partial.Content)
		if text == "" {
			continue
		}
		// Truncate long messages to keep the prompt small.
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		conversationLines = append(conversationLines, partial.Role+": "+text)
		if len(conversationLines) >= 6 {
			break
		}
	}
	if len(conversationLines) == 0 {
		return
	}

	conversation := strings.Join(conversationLines, "\n")

	// Build a minimal request to the current model client.
	client := m.Engine.Client()
	if client == nil {
		return
	}

	systemPrompt := agent.SystemPrompt{
		Blocks: []agent.SystemBlock{{
			Text: "Generate a short, descriptive title (max 60 characters) for this conversation. Output ONLY the title text, nothing else. No quotes, no punctuation at the end.",
		}},
	}

	messages := []agent.Message{
		{Role: agent.UserRole, Content: conversation},
	}

	stream, err := client.Stream(ctx, messages, systemPrompt, nil, 64)
	if err != nil {
		slog.Error("auto title: stream failed", "sessionID", sessionID, "error", err)
		return
	}

	var title strings.Builder
	for ev := range stream {
		if ev.Err != nil {
			slog.Error("auto title: stream error", "sessionID", sessionID, "error", ev.Err)
			return
		}
		if ev.Done {
			break
		}
		if ev.Detail == "text_delta" {
			title.WriteString(ev.Delta)
		}
	}

	generated := strings.TrimSpace(title.String())
	generated = strings.Trim(generated, "\"'`")
	if len(generated) > 80 {
		generated = generated[:77] + "…"
	}
	if generated == "" {
		return
	}

	if err := m.store.Rename(ctx, sessionID, generated); err != nil {
		slog.Error("auto title: rename failed", "sessionID", sessionID, "error", err)
	} else {
		slog.Info("auto title: session renamed", "sessionID", sessionID, "title", generated)
	}
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

func (m *EngineMediator) listMCPServers() {
	if m.mcpManager == nil {
		m.Engine.EmitEvent(protocol.MCPServersListedEvent{})
		return
	}
	statuses := m.mcpManager.Status()
	var servers []protocol.MCPServerInfo
	for _, s := range statuses {
		servers = append(servers, protocol.MCPServerInfo{
			Name:      s.Name,
			Type:      string(s.Type),
			Addr:      s.Addr,
			Connected: s.Connected,
			ToolCount: s.ToolCount,
			Error:     s.Error,
		})
	}
	m.Engine.EmitEvent(protocol.MCPServersListedEvent{Servers: servers})
}

func (m *EngineMediator) connectMCP(name string) {
	if m.mcpManager == nil {
		m.Engine.EmitEvent(protocol.MCPServerStatusChangedEvent{Name: name, Error: "mcp not configured"})
		return
	}
	err := m.mcpManager.ConnectOne(context.Background(), name)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		// Sync registry with new MCP tools
		m.syncMCPTools()
	}
	m.Engine.EmitEvent(protocol.MCPServerStatusChangedEvent{Name: name, Connected: err == nil, Error: errMsg})
}

func (m *EngineMediator) disconnectMCP(name string) {
	if m.mcpManager == nil {
		m.Engine.EmitEvent(protocol.MCPServerStatusChangedEvent{Name: name, Error: "mcp not configured"})
		return
	}
	err := m.mcpManager.DisconnectOne(name)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		// Sync registry after removing MCP tools
		m.syncMCPTools()
	}
	m.Engine.EmitEvent(protocol.MCPServerStatusChangedEvent{Name: name, Connected: false, Error: errMsg})
}

// syncMCPTools rebuilds the registry to match current MCP tool state.
func (m *EngineMediator) syncMCPTools() {
	if m.mcpManager == nil {
		return
	}
	m.Engine.SetMCPTools(m.mcpManager.RegistryTools())
}

func (m *EngineMediator) listTools() {
	defs := m.Engine.Registry().Definitions()
	var tools []protocol.ToolInfo
	for _, d := range defs {
		source := "builtin"
		if strings.HasPrefix(d.Name, "mcp_") {
			parts := strings.SplitN(d.Name, "_", 3)
			if len(parts) >= 3 {
				source = "mcp:" + parts[1]
			}
		}
		tools = append(tools, protocol.ToolInfo{
			Name:        d.Name,
			Description: d.Description,
			Source:      source,
		})
	}
	m.Engine.EmitEvent(protocol.ToolsListedEvent{Tools: tools})
}
