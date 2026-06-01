package ipc

import (
	"encoding/json"
	"reflect"
	"testing"

	"cece/internal/protocol"
)

func TestActionRoundTrip(t *testing.T) {
	tests := []protocol.Action{
		protocol.InputAction{Text: "hello"},
		protocol.ConfirmAction{}, protocol.CancelAction{},
		protocol.ApprovePlanAction{}, protocol.RejectPlanAction{},
		protocol.AnswerQuestionAction{Answers: []protocol.QuestionAnswer{{Question: "q", Selected: []string{"a"}}}},
		protocol.SwitchModelAction{Model: "claude", MaxContextWindow: 200000, Protocol: "anthropic", ConfigName: "main"},
		protocol.CyclePermissionModeAction{},
		protocol.SetPermissionModeAction{Mode: protocol.PermissionModePlan},
		protocol.SetExitTargetModeAction{Mode: protocol.PermissionModeAutoAccept},
		protocol.LoadSessionAction{SessionID: "s1"}, protocol.QueueInputAction{Text: "next"},
		protocol.ListModelsAction{}, protocol.ClearHistoryAction{}, protocol.CompactAction{},
		protocol.TruncateToolResultsAction{}, protocol.RenameSessionAction{SessionID: "s1", Title: "new"},
		protocol.ListMCPAction{}, protocol.ConnectMCPAction{Name: "fs"}, protocol.DisconnectMCPAction{Name: "fs"},
		protocol.ListToolsAction{}, protocol.DryRunRequestAction{Input: "preview"},
	}
	for _, in := range tests {
		line, err := MarshalAction(in)
		if err != nil {
			t.Fatalf("MarshalAction(%T): %v", in, err)
		}
		out, err := UnmarshalClientMessage(line)
		if err != nil {
			t.Fatalf("UnmarshalClientMessage(%T): %v", in, err)
		}
		if reflect.TypeOf(out.Action) != reflect.TypeOf(in) {
			t.Fatalf("roundtrip type = %T, want %T; json=%s", out.Action, in, line)
		}
	}
}

func TestEventRoundTrip(t *testing.T) {
	events := []protocol.Event{
		protocol.SessionCreated{ID: "s1", Title: "title"},
		protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: "hi"}},
		protocol.ModelRequestStarted{Reason: "user", EstimatedInputTokens: 12, APICalls: 1},
		protocol.AssistantStarted{}, protocol.AssistantDelta{Text: "hello"}, protocol.AssistantCompleted{},
		protocol.RunFailed{Err: "boom"},
		protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "t1", Name: "Read", Input: json.RawMessage(`{"path":"x"}`)}}},
		protocol.ToolExecCompleted{ID: "t1", Name: "Read", Result: protocol.ToolResult{Content: "ok"}, ToolCounts: map[string]int{"Read": 1}},
		protocol.PlanApprovalRequested{PlanContent: "# plan", PlanFile: "p.md"},
		protocol.QuestionAsked{CallID: "q1", Questions: []protocol.Question{{Question: "continue?", Options: []protocol.QuestionOption{{Label: "yes"}}}}},
		protocol.TurnCompleted{}, protocol.ModelsLoadedEvent{Models: []protocol.ModelInfo{{ID: "m", MaxContextWindow: 100}}},
		protocol.ModeChangedEvent{Mode: protocol.PermissionModePlan, Message: "Entered plan mode"},
		protocol.SessionLoadedEvent{SessionID: "s1", Model: "m", ContextWindow: 100},
		protocol.MCPServersListedEvent{Servers: []protocol.MCPServerInfo{{Name: "fs", Type: "stdio"}}},
		protocol.MCPServerStatusChangedEvent{Name: "fs", Connected: true},
		protocol.TaskUpdatedEvent{Tasks: []protocol.TaskItem{{Content: "a", ActiveForm: "doing a", Status: "pending"}}},
		protocol.ToolsListedEvent{Tools: []protocol.ToolInfo{{Name: "Read", Source: "builtin"}}},
	}
	for _, in := range events {
		line, err := MarshalEvent(in)
		if err != nil {
			t.Fatalf("MarshalEvent(%T): %v", in, err)
		}
		out, err := UnmarshalServerMessage(line)
		if err != nil {
			t.Fatalf("UnmarshalServerMessage(%T): %v", in, err)
		}
		if reflect.TypeOf(out.Event) != reflect.TypeOf(in) {
			t.Fatalf("roundtrip type = %T, want %T; json=%s", out.Event, in, line)
		}
	}
}

func TestUnknownKindFails(t *testing.T) {
	if _, err := UnmarshalClientMessage([]byte(`{"type":"action","kind":"missing","payload":{}}`)); err == nil {
		t.Fatal("expected unknown action kind error")
	}
	if _, err := UnmarshalServerMessage([]byte(`{"type":"event","kind":"missing","payload":{}}`)); err == nil {
		t.Fatal("expected unknown event kind error")
	}
}
