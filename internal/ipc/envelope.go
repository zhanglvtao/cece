package ipc

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"cece/internal/protocol"
)

type ClientMessage struct {
	Type   string
	Action protocol.Action
}

type ServerMessage struct {
	Type    string
	Event   protocol.Event
	Message string
}

type envelope struct {
	Type    string          `json:"type"`
	Kind    string          `json:"kind,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Message string          `json:"message,omitempty"`
}

var actionKinds = map[string]func() protocol.Action{
	"input":                 func() protocol.Action { return &protocol.InputAction{} },
	"dry_run_request":       func() protocol.Action { return &protocol.DryRunRequestAction{} },
	"confirm":               func() protocol.Action { return &protocol.ConfirmAction{} },
	"cancel":                func() protocol.Action { return &protocol.CancelAction{} },
	"approve_plan":          func() protocol.Action { return &protocol.ApprovePlanAction{} },
	"reject_plan":           func() protocol.Action { return &protocol.RejectPlanAction{} },
	"answer_question":       func() protocol.Action { return &protocol.AnswerQuestionAction{} },
	"switch_model":          func() protocol.Action { return &protocol.SwitchModelAction{} },
	"cycle_permission_mode": func() protocol.Action { return &protocol.CyclePermissionModeAction{} },
	"set_permission_mode":   func() protocol.Action { return &protocol.SetPermissionModeAction{} },
	"set_exit_target_mode":  func() protocol.Action { return &protocol.SetExitTargetModeAction{} },
	"load_session":          func() protocol.Action { return &protocol.LoadSessionAction{} },
	"queue_input":           func() protocol.Action { return &protocol.QueueInputAction{} },
	"dequeue_last_input":    func() protocol.Action { return &protocol.DequeueLastInputAction{} },
	"list_models":           func() protocol.Action { return &protocol.ListModelsAction{} },
	"clear_history":         func() protocol.Action { return &protocol.ClearHistoryAction{} },
	"compact":               func() protocol.Action { return &protocol.CompactAction{} },
	"truncate_tool_results": func() protocol.Action { return &protocol.TruncateToolResultsAction{} },
	"rename_session":        func() protocol.Action { return &protocol.RenameSessionAction{} },
	"auto_title_session":    func() protocol.Action { return &protocol.AutoTitleSessionAction{} },
	"delete_session":        func() protocol.Action { return &protocol.DeleteSessionAction{} },
	"list_mcp":              func() protocol.Action { return &protocol.ListMCPAction{} },
	"connect_mcp":           func() protocol.Action { return &protocol.ConnectMCPAction{} },
	"disconnect_mcp":        func() protocol.Action { return &protocol.DisconnectMCPAction{} },
	"list_tools":            func() protocol.Action { return &protocol.ListToolsAction{} },
}

var eventKinds = map[string]func() protocol.Event{
	"session_created":           func() protocol.Event { return &protocol.SessionCreated{} },
	"user_message_added":        func() protocol.Event { return &protocol.UserMessageAdded{} },
	"system_reminder_added":     func() protocol.Event { return &protocol.SystemReminderAdded{} },
	"model_request_started":     func() protocol.Event { return &protocol.ModelRequestStarted{} },
	"request_dry_run":           func() protocol.Event { return &protocol.RequestDryRunEvent{} },
	"assistant_started":         func() protocol.Event { return &protocol.AssistantStarted{} },
	"assistant_delta":           func() protocol.Event { return &protocol.AssistantDelta{} },
	"assistant_completed":       func() protocol.Event { return &protocol.AssistantCompleted{} },
	"run_failed":                func() protocol.Event { return &protocol.RunFailed{} },
	"stream_started":            func() protocol.Event { return &protocol.StreamStarted{} },
	"stream_event_detail":       func() protocol.Event { return &protocol.StreamEventDetail{} },
	"stream_completed":          func() protocol.Event { return &protocol.StreamCompleted{} },
	"truncation_retry":          func() protocol.Event { return &protocol.TruncationRetry{} },
	"tool_call_started":         func() protocol.Event { return &protocol.ToolCallStarted{} },
	"tool_call_delta":           func() protocol.Event { return &protocol.ToolCallDelta{} },
	"tool_call_completed":       func() protocol.Event { return &protocol.ToolCallCompleted{} },
	"tool_calls_ready":          func() protocol.Event { return &protocol.ToolCallsReady{} },
	"tool_exec_started":         func() protocol.Event { return &protocol.ToolExecStarted{} },
	"tool_exec_delta":           func() protocol.Event { return &protocol.ToolExecDelta{} },
	"tool_exec_completed":       func() protocol.Event { return &protocol.ToolExecCompleted{} },
	"thinking_started":          func() protocol.Event { return &protocol.ThinkingStarted{} },
	"thinking_delta":            func() protocol.Event { return &protocol.ThinkingDelta{} },
	"thinking_completed":        func() protocol.Event { return &protocol.ThinkingCompleted{} },
	"plan_approval_requested":   func() protocol.Event { return &protocol.PlanApprovalRequested{} },
	"question_asked":            func() protocol.Event { return &protocol.QuestionAsked{} },
	"queued_input_promoted":     func() protocol.Event { return &protocol.QueuedInputPromoted{} },
	"compacting":                func() protocol.Event { return &protocol.CompactingEvent{} },
	"compacted":                 func() protocol.Event { return &protocol.CompactedEvent{} },
	"truncated_tool_results":    func() protocol.Event { return &protocol.TruncatedToolResultsEvent{} },
	"pruned":                    func() protocol.Event { return &protocol.PrunedEvent{} },
	"context_nudged":            func() protocol.Event { return &protocol.ContextNudgedEvent{} },
	"turn_completed":            func() protocol.Event { return &protocol.TurnCompleted{} },
	"session_title_generated":   func() protocol.Event { return &protocol.SessionTitleGeneratedEvent{} },
	"session_deleted":           func() protocol.Event { return &protocol.SessionDeletedEvent{} },
	"models_loaded":             func() protocol.Event { return &protocol.ModelsLoadedEvent{} },
	"mode_changed":              func() protocol.Event { return &protocol.ModeChangedEvent{} },
	"mode":                      func() protocol.Event { return &protocol.ModeEvent{} },
	"history_cleared":           func() protocol.Event { return &protocol.HistoryClearedEvent{} },
	"session_loaded":            func() protocol.Event { return &protocol.SessionLoadedEvent{} },
	"mcp_servers_listed":        func() protocol.Event { return &protocol.MCPServersListedEvent{} },
	"mcp_server_status_changed": func() protocol.Event { return &protocol.MCPServerStatusChangedEvent{} },
	"task_updated":              func() protocol.Event { return &protocol.TodoUpdatedEvent{} },
	"subagent_started":          func() protocol.Event { return &protocol.SubAgentStartedEvent{} },
	"subagent_activity":         func() protocol.Event { return &protocol.SubAgentActivityEvent{} },
	"subagent_completed":        func() protocol.Event { return &protocol.SubAgentCompletedEvent{} },
	"subagent_failed":           func() protocol.Event { return &protocol.SubAgentFailedEvent{} },
	"tools_listed":              func() protocol.Event { return &protocol.ToolsListedEvent{} },
}

var actionKindByType = map[reflect.Type]string{}
var eventKindByType = map[reflect.Type]string{}

func init() {
	for kind, factory := range actionKinds {
		actionKindByType[valueType(factory())] = kind
	}
	for kind, factory := range eventKinds {
		eventKindByType[valueType(factory())] = kind
	}
}

func MarshalAction(a protocol.Action) ([]byte, error) {
	kind := actionKindByType[valueType(a)]
	return marshal("action", kind, a)
}

func MarshalEvent(ev protocol.Event) ([]byte, error) {
	kind := eventKindByType[valueType(ev)]
	return marshal("event", kind, ev)
}

func MarshalError(message string) ([]byte, error) {
	return json.Marshal(envelope{Type: "error", Message: message})
}

func UnmarshalClientMessage(line []byte) (ClientMessage, error) {
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return ClientMessage{}, err
	}
	if env.Type != "action" {
		return ClientMessage{}, fmt.Errorf("unexpected client message type %q", env.Type)
	}
	factory, ok := actionKinds[env.Kind]
	if !ok {
		return ClientMessage{}, fmt.Errorf("unknown action kind %q", env.Kind)
	}
	a := factory()
	if len(env.Payload) > 0 {
		if err := json.Unmarshal(env.Payload, a); err != nil {
			return ClientMessage{}, err
		}
	}
	return ClientMessage{Type: env.Type, Action: derefAction(a)}, nil
}

func UnmarshalServerMessage(line []byte) (ServerMessage, error) {
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return ServerMessage{}, err
	}
	if env.Type == "error" {
		return ServerMessage{Type: env.Type, Message: env.Message}, nil
	}
	if env.Type != "event" {
		return ServerMessage{}, fmt.Errorf("unexpected server message type %q", env.Type)
	}
	factory, ok := eventKinds[env.Kind]
	if !ok {
		return ServerMessage{}, fmt.Errorf("unknown event kind %q", env.Kind)
	}
	ev := factory()
	if len(env.Payload) > 0 {
		if err := json.Unmarshal(env.Payload, ev); err != nil {
			return ServerMessage{}, err
		}
	}
	return ServerMessage{Type: env.Type, Event: derefEvent(ev)}, nil
}

func marshal(typ, kind string, payload any) ([]byte, error) {
	if kind == "" {
		return nil, fmt.Errorf("unsupported %s payload %T", typ, payload)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{Type: typ, Kind: kind, Payload: b})
}

func valueType(v any) reflect.Type {
	t := reflect.TypeOf(v)
	if t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func derefAction(a protocol.Action) protocol.Action {
	switch v := a.(type) {
	case *protocol.InputAction:
		return *v
	case *protocol.DryRunRequestAction:
		return *v
	case *protocol.ConfirmAction:
		return *v
	case *protocol.CancelAction:
		return *v
	case *protocol.ApprovePlanAction:
		return *v
	case *protocol.RejectPlanAction:
		return *v
	case *protocol.AnswerQuestionAction:
		return *v
	case *protocol.SwitchModelAction:
		return *v
	case *protocol.CyclePermissionModeAction:
		return *v
	case *protocol.SetPermissionModeAction:
		return *v
	case *protocol.SetExitTargetModeAction:
		return *v
	case *protocol.LoadSessionAction:
		return *v
	case *protocol.QueueInputAction:
		return *v
	case *protocol.DequeueLastInputAction:
		return *v
	case *protocol.ListModelsAction:
		return *v
	case *protocol.ClearHistoryAction:
		return *v
	case *protocol.CompactAction:
		return *v
	case *protocol.TruncateToolResultsAction:
		return *v
	case *protocol.RenameSessionAction:
		return *v
	case *protocol.AutoTitleSessionAction:
		return *v
	case *protocol.DeleteSessionAction:
		return *v
	case *protocol.ListMCPAction:
		return *v
	case *protocol.ConnectMCPAction:
		return *v
	case *protocol.DisconnectMCPAction:
		return *v
	case *protocol.ListToolsAction:
		return *v
	default:
		return a
	}
}

func derefEvent(ev protocol.Event) protocol.Event {
	switch v := ev.(type) {
	case *protocol.SessionCreated:
		return *v
	case *protocol.UserMessageAdded:
		return *v
	case *protocol.SystemReminderAdded:
		return *v
	case *protocol.ModelRequestStarted:
		return *v
	case *protocol.RequestDryRunEvent:
		return *v
	case *protocol.AssistantStarted:
		return *v
	case *protocol.AssistantDelta:
		return *v
	case *protocol.AssistantCompleted:
		return *v
	case *protocol.RunFailed:
		return *v
	case *protocol.StreamStarted:
		return *v
	case *protocol.StreamEventDetail:
		return *v
	case *protocol.StreamCompleted:
		return *v
	case *protocol.TruncationRetry:
		return *v
	case *protocol.ToolCallStarted:
		return *v
	case *protocol.ToolCallDelta:
		return *v
	case *protocol.ToolCallCompleted:
		return *v
	case *protocol.ToolCallsReady:
		return *v
	case *protocol.ToolExecStarted:
		return *v
	case *protocol.ToolExecDelta:
		return *v
	case *protocol.ToolExecCompleted:
		return *v
	case *protocol.ThinkingStarted:
		return *v
	case *protocol.ThinkingDelta:
		return *v
	case *protocol.ThinkingCompleted:
		return *v
	case *protocol.PlanApprovalRequested:
		return *v
	case *protocol.QuestionAsked:
		return *v
	case *protocol.QueuedInputPromoted:
		return *v
	case *protocol.CompactingEvent:
		return *v
	case *protocol.CompactedEvent:
		return *v
	case *protocol.TruncatedToolResultsEvent:
		return *v
	case *protocol.PrunedEvent:
		return *v
	case *protocol.TurnCompleted:
		return *v
	case *protocol.SessionTitleGeneratedEvent:
		return *v
	case *protocol.SessionDeletedEvent:
		return *v
	case *protocol.ModelsLoadedEvent:
		return *v
	case *protocol.ModeChangedEvent:
		return *v
	case *protocol.ModeEvent:
		return *v
	case *protocol.HistoryClearedEvent:
		return *v
	case *protocol.SessionLoadedEvent:
		return *v
	case *protocol.MCPServersListedEvent:
		return *v
	case *protocol.MCPServerStatusChangedEvent:
		return *v
	case *protocol.TodoUpdatedEvent:
		return *v
	case *protocol.SubAgentStartedEvent:
		return *v
	case *protocol.SubAgentActivityEvent:
		return *v
	case *protocol.SubAgentCompletedEvent:
		return *v
	case *protocol.SubAgentFailedEvent:
		return *v
	case *protocol.ToolsListedEvent:
		return *v
	default:
		return ev
	}
}

func KindForAction(a protocol.Action) string { return actionKindByType[valueType(a)] }
func KindForEvent(ev protocol.Event) string  { return eventKindByType[valueType(ev)] }

func snakeName(name, suffix string) string {
	name = strings.TrimSuffix(name, suffix)
	var out []rune
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, '_')
		}
		out = append(out, []rune(strings.ToLower(string(r)))...)
	}
	return string(out)
}
