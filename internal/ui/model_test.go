package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
)

type recordingSender struct {
	inputs  []string
	actions []protocol.Action
	events  chan protocol.Event
}

func newRecordingSender() *recordingSender {
	return &recordingSender{events: make(chan protocol.Event, 8)}
}

func (s *recordingSender) Input(_ context.Context, input string) error {
	s.inputs = append(s.inputs, input)
	return nil
}

func (s *recordingSender) Do(action protocol.Action) {
	s.actions = append(s.actions, action)
}

func (s *recordingSender) Events() <-chan protocol.Event { return s.events }

func TestModelInitialEffortDefaultsToXHigh(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	if m.currentEffort != "xhigh" {
		t.Fatalf("currentEffort = %q, want xhigh", m.currentEffort)
	}
	if !strings.Contains(m.statusBar.Render(120), "xhigh") {
		t.Fatalf("status bar missing xhigh: %q", m.statusBar.Render(120))
	}
}

func TestModelSetDefaultEffort(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.SetDefaultEffort("medium")
	if m.currentEffort != "medium" {
		t.Fatalf("currentEffort = %q, want medium", m.currentEffort)
	}
	if !strings.Contains(m.statusBar.Render(120), "medium") {
		t.Fatalf("status bar missing medium: %q", m.statusBar.Render(120))
	}
}

func TestEngineReadyEventSyncsEffort(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.EngineReadyEvent{Model: "opus", ContextWindow: 123, Effort: "xhigh"})
	if m.modelName != "opus" || m.contextWindow != 123 || m.currentEffort != "xhigh" {
		t.Fatalf("model/context/effort = %s/%d/%s, want opus/123/xhigh", m.modelName, m.contextWindow, m.currentEffort)
	}
	if !strings.Contains(m.statusBar.Render(120), "xhigh") {
		t.Fatalf("status bar missing xhigh: %q", m.statusBar.Render(120))
	}
}

func TestObservatoryServerStartedRendersInTitleBar(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.ObservatoryServerStartedEvent{URL: "http://127.0.0.1:49321", Host: "127.0.0.1", Port: 49321})
	if got := m.ObservatoryURLForTest(); got != "http://127.0.0.1:49321" {
		t.Fatalf("observatoryURL = %q", got)
	}
	view := stripAnsi(m.titleBarView())
	if !strings.Contains(view, "obs:http://127.0.0.1:49321") {
		t.Fatalf("title bar missing obs URL: %q", view)
	}
}

func TestObservatorySnapshotReflectsTUIState(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.busy = true
	m.status = "Streaming"
	m.queued = []string{"next"}
	snap := m.ObservatorySnapshotForTest()
	if snap.Scope != "tui:client" || snap.ActivePhase != "busy" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if len(snap.Nodes) != 1 || snap.Nodes[0].Status != "active" || snap.Nodes[0].Meta["queued"] != "1" {
		t.Fatalf("snapshot node = %+v", snap.Nodes)
	}
}

func TestApplyEventBuildsTranscriptAndClearsBusy(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: "hi"}})
	m.ApplyEventForTest(protocol.AssistantStarted{})
	m.ApplyEventForTest(protocol.AssistantDelta{Text: "hello"})
	m.ApplyEventForTest(protocol.AssistantDelta{Text: " there"})
	m.ApplyEventForTest(protocol.TurnCompleted{})

	if m.busy {
		t.Fatal("busy = true after TurnCompleted")
	}
	view := m.transcript.render(80, m.styles)
	plain := stripAnsi(view)
	if !containsAll(plain, "hi", "hello there") {
		t.Fatalf("transcript missing expected content:\n%s", view)
	}
	if !strings.Contains(plain, "♦ You") {
		t.Fatalf("user label missing:\n%s", view)
	}
	if !strings.Contains(plain, "◊ Cece") {
		t.Fatalf("assistant label missing:\n%s", view)
	}
	if strings.Contains(view, "[you]") || strings.Contains(view, "[cece]") {
		t.Fatalf("transcript labels should not use brackets:\n%s", view)
	}
	if !strings.Contains(plain, "\n  hi") {
		t.Fatalf("user input should be indented:\n%s", view)
	}
	if !strings.Contains(plain, "\n  hello there") {
		t.Fatalf("cece output should be indented:\n%s", view)
	}
	bodyStart := strings.Index(view, "hello there")
	if bodyStart < 0 {
		t.Fatalf("cece output body missing:\n%s", view)
	}
	bodyPrefix := view[max(0, bodyStart-12):bodyStart]
	if !strings.Contains(bodyPrefix, "\x1b[") {
		t.Fatalf("cece output body should contain ANSI styling:\n%s", view)
	}
}

func TestViewAddsHorizontalPadding(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.width = 80
	m.height = 24
	m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: "hi"}})
	full := stripAnsi(m.View().Content)
	lines := strings.Split(full, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "  ") {
		t.Fatalf("view should have left padding:\n%s", full)
	}
}

func TestToolConfirmDispatchesActions(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "1", Name: "Edit", Input: json.RawMessage(`{"file":"a.go"}`)}}})
	if m.modal.kind != modalConfirmTools {
		t.Fatalf("modal = %v, want confirm tools", m.modal.kind)
	}
	m.handleModalKey(keyMsg("y"))
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.ConfirmAction); !ok {
		t.Fatalf("last action = %T, want ConfirmAction", sender.actions[len(sender.actions)-1])
	}

	m.ApplyEventForTest(protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "1", Name: "Edit"}}})
	m.handleModalKey(keyMsg("shift+tab"))
	if m.modal.active() {
		t.Fatal("modal should be closed after enabling auto-accept")
	}
	if len(sender.actions) < 3 {
		t.Fatalf("actions = %d, want at least 3", len(sender.actions))
	}
	setMode, ok := sender.actions[len(sender.actions)-2].(protocol.SetPermissionModeAction)
	if !ok {
		t.Fatalf("second last action = %T, want SetPermissionModeAction", sender.actions[len(sender.actions)-2])
	}
	if setMode.Mode != protocol.PermissionModeAutoAccept {
		t.Fatalf("mode = %q, want auto-accept", setMode.Mode)
	}
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.ConfirmAction); !ok {
		t.Fatalf("last action = %T, want ConfirmAction", sender.actions[len(sender.actions)-1])
	}

	m.ApplyEventForTest(protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "1", Name: "Edit"}}})
	m.handleModalKey(keyMsg("n"))
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.RejectToolCallsAction); !ok {
		t.Fatalf("last action = %T, want RejectToolCallsAction", sender.actions[len(sender.actions)-1])
	}
}

func TestPlanApprovalDispatchesActions(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.PlanApprovalRequested{PlanFile: "plan.md", PlanContent: "# Plan"})
	if m.modal.kind != modalApprovePlan {
		t.Fatalf("modal = %v, want approve plan", m.modal.kind)
	}
	rendered := m.transcript.render(80, m.styles)
	if !containsAll(rendered, "plan.md", "Plan") {
		t.Fatalf("plan content not rendered:\n%s", rendered)
	}
	m.handleModalKey(keyMsg("y"))
	if len(sender.actions) < 2 {
		t.Fatalf("actions = %d, want at least 2", len(sender.actions))
	}
	setMode, ok := sender.actions[len(sender.actions)-2].(protocol.SetExitTargetModeAction)
	if !ok {
		t.Fatalf("second last action = %T, want SetExitTargetModeAction", sender.actions[len(sender.actions)-2])
	}
	if setMode.Mode != protocol.PermissionModeDefault {
		t.Fatalf("mode = %q, want default", setMode.Mode)
	}
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.ApprovePlanAction); !ok {
		t.Fatalf("last action = %T, want ApprovePlanAction", sender.actions[len(sender.actions)-1])
	}

	m.ApplyEventForTest(protocol.PlanApprovalRequested{PlanFile: "plan.md"})
	m.handleModalKey(keyMsg("n"))
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.RejectPlanAction); !ok {
		t.Fatalf("last action = %T, want RejectPlanAction", sender.actions[len(sender.actions)-1])
	}
	if m.mode != protocol.PermissionModePlan {
		t.Fatalf("mode = %q, want plan", m.mode)
	}
}

func TestQuestionModalBuildsAnswerAction(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{{
		Question: "Pick one",
		Options:  []protocol.QuestionOption{{Label: "A"}, {Label: "B"}},
	}}})
	m.handleModalKey(keyMsg("down"))
	m.handleModalKey(keyMsg("enter"))

	action, ok := sender.actions[len(sender.actions)-1].(protocol.AnswerQuestionAction)
	if !ok {
		t.Fatalf("last action = %T, want AnswerQuestionAction", sender.actions[len(sender.actions)-1])
	}
	if got := action.Answers[0].Selected; len(got) != 1 || got[0] != "B" {
		t.Fatalf("selected = %v, want [B]", got)
	}
}

func TestPlanApprovalShiftTabAutoAccept(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.PlanApprovalRequested{PlanFile: "plan.md", PlanContent: "# Plan"})
	m.handleModalKey(keyMsg("shift+tab"))
	if m.modal.active() {
		t.Fatal("modal should be closed after shift+tab")
	}
	if len(sender.actions) < 2 {
		t.Fatalf("actions = %d, want at least 2", len(sender.actions))
	}
	setMode, ok := sender.actions[len(sender.actions)-2].(protocol.SetExitTargetModeAction)
	if !ok {
		t.Fatalf("second last action = %T, want SetExitTargetModeAction", sender.actions[len(sender.actions)-2])
	}
	if setMode.Mode != protocol.PermissionModeAutoAccept {
		t.Fatalf("mode = %q, want auto-accept", setMode.Mode)
	}
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.ApprovePlanAction); !ok {
		t.Fatalf("last action = %T, want ApprovePlanAction", sender.actions[len(sender.actions)-1])
	}
}

func TestQuestionShiftTabAutoAnswer(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{
		{Question: "Pick one", Options: []protocol.QuestionOption{{Label: "A"}, {Label: "B"}}},
		{Question: "Pick another", Options: []protocol.QuestionOption{{Label: "X"}, {Label: "Y"}}},
	}})
	// Move cursor on first question to "B"
	m.handleModalKey(keyMsg("down"))
	// Move cursor on second question to "Y" (need to go to q2 first)
	m.handleModalKey(keyMsg("right"))
	m.handleModalKey(keyMsg("down"))
	// shift+tab should auto-answer all questions with current cursor positions
	m.handleModalKey(keyMsg("shift+tab"))

	action, ok := sender.actions[len(sender.actions)-1].(protocol.AnswerQuestionAction)
	if !ok {
		t.Fatalf("last action = %T, want AnswerQuestionAction", sender.actions[len(sender.actions)-1])
	}
	if got := action.Answers[0].Selected; len(got) != 1 || got[0] != "B" {
		t.Fatalf("q0 selected = %v, want [B]", got)
	}
	if got := action.Answers[1].Selected; len(got) != 1 || got[0] != "Y" {
		t.Fatalf("q1 selected = %v, want [Y]", got)
	}
}

func TestModelPickerDispatchesSwitchModel(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "old", "/tmp")
	m.ApplyEventForTest(protocol.ModelsLoadedEvent{Models: []protocol.ModelInfo{
		{ID: "old", DisplayName: "Old"},
		{ID: "new", DisplayName: "New", MaxContextWindow: 123, Provider: "p", Protocol: "aiden"},
	}})
	m.handleModalKey(keyMsg("down"))
	m.handleModalKey(keyMsg("enter"))

	action, ok := sender.actions[len(sender.actions)-1].(protocol.SwitchModelAction)
	if !ok {
		t.Fatalf("last action = %T, want SwitchModelAction", sender.actions[len(sender.actions)-1])
	}
	if action.Model != "new" || action.MaxContextWindow != 123 || action.Protocol != "aiden" {
		t.Fatalf("action = %+v", action)
	}
	if m.modelName != "new" || m.contextWindow != 123 {
		t.Fatalf("model/context = %s/%d", m.modelName, m.contextWindow)
	}
}

func TestToolResultRequestSummaryRendersOnToolLine(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "tool-1", Name: "Grep"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "tool-1", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "tool-1", Name: "Grep", Result: protocol.ToolResult{Content: "match"}})
	m.ApplyEventForTest(protocol.ModelRequestStarted{Reason: "tool_result", EstimatedInputTokens: 80981, ToolResults: []string{"Grep"}})

	rendered := m.transcript.render(160, m.styles)
	if strings.Contains(rendered, "[tool_result]") {
		t.Fatalf("tool_result should not render as standalone block:\n%s", rendered)
	}
	if strings.Contains(rendered, "[Grep]") {
		t.Fatalf("tool label should not use brackets:\n%s", rendered)
	}
	if !containsAll(rendered, "Grep", "estimated input: 80981", "tool results: Grep") {
		t.Fatalf("tool line missing request summary:\n%s", rendered)
	}
}

func TestEditToolRendersDiffWithoutParams(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	input := json.RawMessage(`{"path":"/tmp/a.go","old_string":"very long old string that should not appear","new_string":"very long new string that should not appear"}`)
	diff := "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new"

	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "tool-edit", Name: "Edit"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "tool-edit", Name: "Edit", Input: input})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "tool-edit", Name: "Edit", Result: protocol.ToolResult{Content: diff}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	if !containsAll(rendered, "Edit", "ok:", "--- a/a.go", "+new") {
		t.Fatalf("edit diff not rendered:\n%s", rendered)
	}
	if strings.Contains(rendered, "old_string") || strings.Contains(rendered, "new_string") || strings.Contains(rendered, "very long") {
		t.Fatalf("edit params should not be rendered:\n%s", rendered)
	}
	if strings.Contains(rendered, "\n---\n") {
		t.Fatalf("edit result should not be appended after parameter preview:\n%s", rendered)
	}
}

func TestWriteToolRendersDiffWithoutParamsAndLimitsLines(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	input := json.RawMessage(`{"path":"/tmp/report.txt","content":"very long content that should not appear in parameter preview"}`)
	lines := []string{
		"--- a/report.txt",
		"+++ b/report.txt",
		"@@ -1,3 +1,9 @@",
		" old1",
		"-old2",
		"+new2",
		" line3",
		"+line4",
		"+line5",
		"+line6",
		"+line7",
		"+line8",
	}
	diff := strings.Join(lines, "\n")

	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "tool-write", Name: "Write"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "tool-write", Name: "Write", Input: input})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "tool-write", Name: "Write", Result: protocol.ToolResult{Content: diff}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	if !containsAll(rendered, "Write", "ok:", "--- a/report.txt", "+line4", "... 4 lines hidden ...", "... truncated ...") {
		t.Fatalf("write diff not rendered/truncated:\n%s", rendered)
	}
	for _, hidden := range []string{"content", "path", "very long content", "/tmp/report.txt", "+line5", "+line6", "+line7", "+line8"} {
		if strings.Contains(rendered, hidden) {
			t.Fatalf("write params or overflow diff should not be rendered (%q):\n%s", hidden, rendered)
		}
	}
	if strings.Contains(rendered, "\n---\n") {
		t.Fatalf("write result should not be appended after parameter preview:\n%s", rendered)
	}
}

func TestTodoToolRendersCountWithoutParams(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	input := json.RawMessage(`{"todos":[{"content":"Fix UI","activeForm":"Fixing UI","status":"in_progress"},{"content":"Run tests","activeForm":"Running tests","status":"pending"}]}`)

	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "tool-todo", Name: "Todo"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "tool-todo", Name: "Todo", Input: input})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "tool-todo", Name: "Todo", Result: protocol.ToolResult{Content: "Tasks updated: 2 todos."}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	if !containsAll(rendered, "Todo", "2 todos", "Fix UI", "Run tests") {
		t.Fatalf("todo summary not rendered:\n%s", rendered)
	}
	for _, hidden := range []string{"activeForm", "content", "status", "Fixing UI", "Running tests", "in_progress", "pending"} {
		if strings.Contains(rendered, hidden) {
			t.Fatalf("todo params should not be rendered (%q):\n%s", hidden, rendered)
		}
	}
}

func TestToolBlocksSingleGapBetweenTools(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	// Grep (quiet tool)
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t1", Name: "Grep"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t1", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t1", Name: "Grep", Result: protocol.ToolResult{Content: "match"}})
	// Read (quiet tool)
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t2", Name: "Read"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t2", Name: "Read", Input: json.RawMessage(`{"path":"/tmp/a.go","limit":30,"offset":100}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t2", Name: "Read", Result: protocol.ToolResult{Content: "content"}})
	// Bash (exec tool)
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t3", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t3", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)})
	m.ApplyEventForTest(protocol.ToolExecStarted{ID: "t3", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t3", Name: "Bash", Result: protocol.ToolResult{Content: "file1\nfile2\n"}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	// Grep and Read are consecutive quiet tools with ✓ — should have single \n between labels
	if !strings.Contains(rendered, "Grep pattern: TODO ✓\nRead path: /tmp/a.go limit: 30 offset: 100 ✓") {
		t.Fatalf("expected single newline between consecutive quiet tools:\n%s", rendered)
	}
	// Read (quiet ✓) followed by Bash label on next block — single \n gap (tool-to-tool)
	// Bash block starts with its label, then a newline, then content, then status.
	if strings.Contains(rendered, "Read path: /tmp/a.go limit: 30 offset: 100 ✓\n\nBash command: ls") {
		t.Fatalf("expected single newline (not double) between Read and Bash tools:\n%s", rendered)
	}
	if !strings.Contains(rendered, "offset: 100 ✓\nBash command: ls") {
		t.Fatalf("expected Read followed immediately by Bash with single newline:\n%s", rendered)
	}
}

func TestGapBetweenToolAndAssistantIsDouble(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	// Grep (quiet tool)
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t1", Name: "Grep"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t1", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t1", Name: "Grep", Result: protocol.ToolResult{Content: "match"}})
	// Assistant message
	m.ApplyEventForTest(protocol.ModelRequestStarted{Reason: "tool_result", EstimatedInputTokens: 10, ToolResults: []string{"Grep"}})
	m.ApplyEventForTest(protocol.AssistantDelta{Text: "hello"})
	m.ApplyEventForTest(protocol.AssistantCompleted{})
	// Another tool (Bash)
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t2", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t2", Name: "Bash", Input: json.RawMessage(`{"command":"pwd"}`)})
	m.ApplyEventForTest(protocol.ToolExecStarted{ID: "t2", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t2", Name: "Bash", Result: protocol.ToolResult{Content: "/tmp\n"}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	// The Markdown renderer wraps lines to width, so anchor on the boundaries
	// by looking for the visual gap only (no line-match anchor on "hello").
	// Grep tool → assistant body: must keep double newline (semantic boundary)
	if !strings.Contains(rendered, "tool results: Grep ✓\n\n") {
		t.Fatalf("expected double newline between tool and assistant body:\n%s", rendered)
	}
	// assistant body → Bash (tool): also double newline.
	// Rendered text ends with "hello" + trailing wrap spaces then \n\n + Bash label.
	// Look for the \n\nBash boundary instead.
	if !strings.Contains(rendered, "\n\nBash command: pwd") {
		t.Fatalf("expected double newline between assistant and next tool:\n%s", rendered)
	}
}

func TestToolChainBlocksStayTightAcrossInfoAndGate(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t1", Name: "Grep"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t1", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t1", Name: "Grep", Result: protocol.ToolResult{Content: "match"}})
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t2", Name: "Read"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t2", Name: "Read", Input: json.RawMessage(`{"path":"/tmp/a.go"}`)})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t2", Name: "Read", Result: protocol.ToolResult{Content: "content"}})
	m.ApplyEventForTest(protocol.ModelRequestStarted{Reason: "tool_result", EstimatedInputTokens: 42, ToolResults: []string{"MissingTool"}})
	m.ApplyEventForTest(protocol.ToolCallStarted{ID: "t3", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolCallCompleted{ID: "t3", Name: "Bash", Input: json.RawMessage(`{"command":"pwd"}`)})
	m.ApplyEventForTest(protocol.ToolExecStarted{ID: "t3", Name: "Bash"})
	m.ApplyEventForTest(protocol.ToolExecCompleted{ID: "t3", Name: "Bash", Result: protocol.ToolResult{Content: "/tmp\n"}})

	rendered := stripAnsi(m.transcript.render(160, m.styles))
	if strings.Contains(rendered, "Grep pattern: TODO ✓\n\nCompletion gate") {
		t.Fatalf("expected tool and completion gate blocks to stay tight:\n%s", rendered)
	}
	if strings.Contains(rendered, "Completion gate\n  hook 1/3: blocked → continue\n\nRead path: /tmp/a.go ✓") {
		t.Fatalf("expected completion gate and following tool blocks to stay tight:\n%s", rendered)
	}
	if strings.Contains(rendered, "Read path: /tmp/a.go ✓\n\nTool_result") {
		t.Fatalf("expected tool and tool_result info blocks to stay tight:\n%s", rendered)
	}
	if strings.Contains(rendered, "estimated input: 42 | tool results: MissingTool\n\nBash command: pwd") {
		t.Fatalf("expected tool_result info and following tool blocks to stay tight:\n%s", rendered)
	}

	}

func TestSessionLoadedRebuildsTranscript(t *testing.T) {
	m := NewModel(nil, "old", "/tmp")
	m.ApplyEventForTest(protocol.SessionLoadedEvent{
		SessionID:     "sess1",
		Model:         "new",
		ContextWindow: 200,
		LastInput:     10,
		TotalInput:    20,
		TotalOutput:   5,
		History: []protocol.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ContentBlocks: []protocol.ContentBlock{{Type: protocol.TextContentType, Text: "answer"}}},
		},
	})

	if m.currentSessionID != "sess1" || m.modelName != "new" || m.contextWindow != 200 {
		t.Fatalf("session/model/context not updated: %s %s %d", m.currentSessionID, m.modelName, m.contextWindow)
	}
	if m.transcript.inputTokens != 20 || m.transcript.outputTokens != 5 || m.transcript.contextUsed != 10 {
		t.Fatalf("tokens = %d/%d context=%d", m.transcript.inputTokens, m.transcript.outputTokens, m.transcript.contextUsed)
	}
	if !containsAll(m.transcript.render(80, m.styles), "hi", "answer") {
		t.Fatalf("history not rendered:\n%s", m.transcript.render(80, m.styles))
	}
}

func TestHistoryClearedEventResetsTranscriptAndContextGauge(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp", 200000)
	m.ApplyEventForTest(protocol.SessionLoadedEvent{
		SessionID:     "sess1",
		Model:         "sonnet",
		ContextWindow: 200000,
		LastInput:     180000,
		TotalInput:    180000,
		TotalOutput:   5000,
		History: []protocol.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ContentBlocks: []protocol.ContentBlock{{Type: protocol.TextContentType, Text: "answer"}}},
		},
	})

	beforeStatus := stripAnsi(m.statusBar.Render(120))
	if !strings.Contains(beforeStatus, "20K/200K 10%") {
		t.Fatalf("status before clear = %q, want remaining ctx 20K/200K 10%%", beforeStatus)
	}
	if !containsAll(m.transcript.render(80, m.styles), "hi", "answer") {
		t.Fatalf("history before clear not rendered:\n%s", m.transcript.render(80, m.styles))
	}

	m.ApplyEventForTest(protocol.HistoryClearedEvent{})

	if m.status != "Cleared" {
		t.Fatalf("status = %q, want Cleared", m.status)
	}
	afterTranscript := m.transcript.render(80, m.styles)
	if strings.Contains(afterTranscript, "hi") || strings.Contains(afterTranscript, "answer") {
		t.Fatalf("history still rendered after clear:\n%s", afterTranscript)
	}
	afterStatus := stripAnsi(m.statusBar.Render(120))
	if !strings.Contains(afterStatus, "200K/200K 100%") {
		t.Fatalf("status after clear = %q, want remaining ctx 200K/200K 100%%", afterStatus)
	}
}

func TestCompactedEventFailureShowsError(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.CompactedEvent{MessagesBefore: 10, MessagesAfter: 10, Err: "summary boom"})

	if !strings.Contains(m.status, "Compact failed: summary boom") {
		t.Fatalf("status = %q, want compact failure", m.status)
	}
	plain := stripAnsi(m.transcript.render(80, m.styles))
	if !strings.Contains(plain, "Compact failed: summary boom") || strings.Contains(plain, "Not enough messages") {
		t.Fatalf("transcript = %q", plain)
	}
}

func TestCompressionFallbackEventsUpdateContextGauge(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp", 1000)

	m.ApplyEventForTest(protocol.TruncatedToolResultsEvent{TruncatedCount: 2, TokensBefore: 900, TokensAfter: 700})
	if m.transcript.contextUsed != 700 {
		t.Fatalf("context after trim = %d, want 700", m.transcript.contextUsed)
	}
	m.ApplyEventForTest(protocol.PrunedEvent{PrunedTurns: 3, TokensBefore: 700, TokensAfter: 100})
	if m.transcript.contextUsed != 100 {
		t.Fatalf("context after prune = %d, want 100", m.transcript.contextUsed)
	}
}

func TestSlashModelAndSkill(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.input.SetValue("/model")
	_, cmd := m.handleKey(keyMsg("enter"))
	if cmd != nil {
		_ = cmd()
	}
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.ListModelsAction); !ok {
		t.Fatalf("last action = %T, want ListModelsAction", sender.actions[len(sender.actions)-1])
	}

	store := skill.NewStore([]*skill.Skill{{
		Name:         "demo",
		Description:  "demo skill",
		Instructions: "Do demo",
	}})
	store.SetAllEnabled(true)
	m.SetSkillStore(store)
	m.input.SetValue("/demo with args")
	_, cmd = m.handleKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("expected submit command")
	}
	_ = cmd()
	if len(sender.inputs) == 0 || !containsAll(sender.inputs[len(sender.inputs)-1], "<loaded_skill>", "demo", "with args") {
		t.Fatalf("skill input = %q", sender.inputs)
	}
}

func TestDoubleSlashSendsPlainInput(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.input.SetValue("//")
	_, cmd := m.handleKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("expected submit command")
	}
	_ = cmd()
	if len(sender.inputs) != 1 || sender.inputs[0] != "//" {
		t.Fatalf("inputs = %v, want [//]", sender.inputs)
	}
	if got := m.StatusForTest(); got == "Unknown command: //" {
		t.Fatalf("status = %q, should not be treated as slash command", got)
	}
}

func TestBusyInputQueuesAction(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.busy = true
	m.input.SetValue("second")
	m.handleSend()
	if len(m.queued) != 1 || m.queued[0] != "second" {
		t.Fatalf("queued = %v", m.queued)
	}
	if action, ok := sender.actions[len(sender.actions)-1].(protocol.QueueInputAction); !ok || action.Text != "second" {
		t.Fatalf("last action = %#v, want QueueInputAction(second)", sender.actions[len(sender.actions)-1])
	}
}

func TestQuestionCancelSuspendsInsteadOfRejecting(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{{Question: "继续吗？"}}})

	m.handleModalKey(keyMsg("esc"))

	if m.modal.active() {
		t.Fatal("modal should close after suspending question")
	}
	if got := m.status; got != "Question suspended" {
		t.Fatalf("status = %q, want Question suspended", got)
	}
	if len(sender.actions) == 0 {
		t.Fatal("expected suspend action")
	}
	if _, ok := sender.actions[len(sender.actions)-1].(protocol.SuspendQuestionAction); !ok {
		t.Fatalf("last action = %T, want SuspendQuestionAction", sender.actions[len(sender.actions)-1])
	}
}

func TestSuspendedQuestionSendResumesInsteadOfQueueing(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.busy = true
	m.status = "Question suspended"
	m.input.SetValue("补充说明")

	m.handleSend()

	if len(m.queued) != 0 {
		t.Fatalf("queued = %v, want empty", m.queued)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("input = %q, want empty after resume send", got)
	}
	if len(sender.actions) == 0 {
		t.Fatal("expected resume action")
	}
	if action, ok := sender.actions[len(sender.actions)-1].(protocol.ResumeQuestionAction); !ok || action.Text != "补充说明" {
		t.Fatalf("last action = %#v, want ResumeQuestionAction(补充说明)", sender.actions[len(sender.actions)-1])
	}
}

func TestViewportPreservesManualScrollDuringStreaming(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 60, Height: 10})
	for i := 0; i < 12; i++ {
		m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: strings.Repeat("old message\n", 3)}})
	}
	if !m.viewport.AtBottom() {
		t.Fatal("viewport should follow initial transcript to bottom")
	}

	m.viewport.ScrollUp(4)
	before := m.viewport.YOffset()
	if before == 0 || m.viewport.AtBottom() {
		t.Fatalf("test setup failed: offset=%d atBottom=%v", before, m.viewport.AtBottom())
	}

	m.ApplyEventForTest(protocol.AssistantDelta{Text: strings.Repeat("streaming update\n", 4)})
	if got := m.viewport.YOffset(); got != before {
		t.Fatalf("streaming forced viewport offset from %d to %d", before, got)
	}
}

func TestViewportScrollKeysMoveByLineAndPage(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 12; i++ {
		m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: strings.Repeat("old message\n", 3)}})
	}
	bottom := m.viewport.YOffset()
	if bottom == 0 {
		t.Fatal("test setup failed: transcript is not scrollable")
	}

	m.handleKey(keyMsg("ctrl+up"))
	afterLine := m.viewport.YOffset()
	if afterLine != bottom-1 {
		t.Fatalf("ctrl+up offset = %d, want %d", afterLine, bottom-1)
	}
	m.resize()
	if !strings.Contains(m.statusBar.Render(m.width), "scroll:") {
		t.Fatalf("statusbar should show scroll position while not at bottom")
	}

	m.handleKey(keyMsg("pgup"))
	afterPage := m.viewport.YOffset()
	if afterPage >= afterLine {
		t.Fatalf("pgup offset = %d, want less than %d", afterPage, afterLine)
	}
	m.handleKey(keyMsg("pgdown"))
	if got := m.viewport.YOffset(); got <= afterPage {
		t.Fatalf("pgdown offset = %d, want greater than %d", got, afterPage)
	}
}

func TestViewportCtrlDownUsesModifiersEvenWithDraftInput(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 60, Height: 10})
	for i := 0; i < 12; i++ {
		m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: strings.Repeat("old message\n", 3)}})
	}
	m.viewport.ScrollUp(3)
	before := m.viewport.YOffset()
	if before == 0 || m.viewport.AtBottom() {
		t.Fatalf("test setup failed: offset=%d atBottom=%v", before, m.viewport.AtBottom())
	}

	m.input.SetValue("draft")
	m.handleKey(tea.KeyPressMsg(tea.Key{Text: "down", Code: tea.KeyDown, Mod: tea.ModCtrl}))
	if got := m.viewport.YOffset(); got != before+1 {
		t.Fatalf("ctrl+down with draft offset = %d, want %d", got, before+1)
	}
}

func TestPopupAllowsViewportMouseWheelScroll(t *testing.T) {
	m := newScrollableModel(t)
	m.ApplyEventForTest(protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "1", Name: "Edit"}}})
	if !m.modal.active() {
		t.Fatal("test setup failed: modal is not active")
	}

	bottom := m.viewport.YOffset()
	m.update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if got := m.viewport.YOffset(); got >= bottom {
		t.Fatalf("mouse wheel did not scroll chat while popup active: got offset %d, want less than %d", got, bottom)
	}
}

func TestPopupAllowsViewportModifierScrollKeys(t *testing.T) {
	m := newScrollableModel(t)
	m.ApplyEventForTest(protocol.ToolCallsReady{Calls: []protocol.ToolUseBlock{{ID: "1", Name: "Edit"}}})
	if !m.modal.active() {
		t.Fatal("test setup failed: modal is not active")
	}

	bottom := m.viewport.YOffset()
	m.handleKey(keyMsg("ctrl+up"))
	if got := m.viewport.YOffset(); got != bottom-1 {
		t.Fatalf("ctrl+up did not scroll chat while popup active: got offset %d, want %d", got, bottom-1)
	}
}

func TestSessionPickerDispatchesLoadSession(t *testing.T) {
	sender := newRecordingSender()
	store := &fakeSessionStore{sessions: []session.Session{
		{ID: "a", Title: "A", UpdatedAt: time.Now()},
		{ID: "b", Title: "B", UpdatedAt: time.Now()},
	}}
	m := NewModel(sender, "sonnet", "/tmp")
	m.SetSessions(store)
	m.input.SetValue("/resume")
	m.handleSend()
	if m.modal.kind != modalSessionPicker {
		t.Fatalf("modal = %v, want session picker", m.modal.kind)
	}
	m.handleModalKey(keyMsg("down"))
	m.handleModalKey(keyMsg("enter"))
	action, ok := sender.actions[len(sender.actions)-1].(protocol.LoadSessionAction)
	if !ok || action.SessionID != "b" {
		t.Fatalf("last action = %#v, want LoadSessionAction(b)", sender.actions[len(sender.actions)-1])
	}
}

func TestModelSyncsModeToStatusBar(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.SetDefaultMode("plan")
	got := stripAnsi(m.statusBar.Render(120))
	parts := strings.Split(got, " | ")
	if parts[0] != "Plan" {
		t.Fatalf("default mode statusbar column = %q, want %q", parts[0], "Plan")
	}

	m.ApplyEventForTest(protocol.ModeChangedEvent{Mode: protocol.PermissionModeAutoAccept, Message: "Auto-accept mode"})
	got = stripAnsi(m.statusBar.Render(120))
	parts = strings.Split(got, " | ")
	if parts[0] != "Auto" {
		t.Fatalf("changed mode statusbar column = %q, want %q", parts[0], "Auto")
	}
}

func TestStatusRendersAboveInput(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})

	view := stripAnsi(m.View().Content)
	statusIdx := strings.Index(view, "Ready")
	inputIdx := strings.Index(view, "Send a message")
	metricsIdx := strings.Index(view, "sonnet")
	if statusIdx < 0 {
		t.Fatalf("missing status in view")
	}
	if inputIdx < 0 {
		t.Fatalf("missing input in view")
	}
	if metricsIdx < 0 {
		t.Fatalf("missing metrics bar in view")
	}
	if statusIdx > inputIdx {
		t.Fatalf("status should be above input; statusIdx=%d inputIdx=%d", statusIdx, inputIdx)
	}
	if strings.Contains(view[metricsIdx:], "Ready") {
		t.Fatalf("bottom metrics bar should not contain status")
	}
}

func TestHeadlineStatusSweepMovesAcrossFrames(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m.busy = true
	m.status = "Requesting"
	m.requestSweepActive = true
	m.requestStartTime = time.Now().Add(-2 * time.Second)

	m.statusFrame = 0
	first := m.headlineView()
	m.statusFrame = 1
	second := m.headlineView()
	m.statusFrame = 2
	third := m.headlineView()

	if stripAnsi(first) != stripAnsi(second) || stripAnsi(second) != stripAnsi(third) {
		t.Fatalf("sweep should not change visible status text:\n%s\n%s\n%s", stripAnsi(first), stripAnsi(second), stripAnsi(third))
	}
	if first == second || second == third {
		t.Fatalf("sweep should move ANSI highlight across frames")
	}
	plain := stripAnsi(first)
	if strings.HasPrefix(plain, "- ") || strings.HasPrefix(plain, "\\ ") || strings.HasPrefix(plain, "| ") || strings.HasPrefix(plain, "/ ") {
		t.Fatalf("headline should not include spinner prefix: %q", plain)
	}
	if !strings.Contains(plain, "Requesting") || !strings.Contains(plain, "2s") {
		t.Fatalf("headline missing status or elapsed duration: %q", plain)
	}
}

func TestQuestionAskedStopsRequestSweepImmediately(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m.ApplyEventForTest(protocol.ModelRequestStarted{})
	m.requestStartTime = time.Now().Add(-2 * time.Second)

	m.statusFrame = 0
	before := m.headlineView()
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{{Question: "Pick one", Options: []protocol.QuestionOption{{Label: "A"}, {Label: "B"}}}}})
	m.statusFrame = 4
	after := m.headlineView()

	if before == after {
		t.Fatalf("expected headline to change after question asked")
	}
	if m.requestSweepActive {
		t.Fatal("request sweep should stop after QuestionAsked")
	}
	if stripAnsi(after) != "Answer question" {
		t.Fatalf("headline = %q, want Answer question", stripAnsi(after))
	}
}

func TestPlanApprovalStopsRequestSweepImmediately(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m.ApplyEventForTest(protocol.AssistantStarted{})
	m.statusFrame = 0
	before := m.headlineView()
	m.ApplyEventForTest(protocol.PlanApprovalRequested{PlanFile: "plan.md"})
	m.statusFrame = 4
	after := m.headlineView()

	if before == after {
		t.Fatalf("expected headline to change after plan approval request")
	}
	if m.requestSweepActive {
		t.Fatal("request sweep should stop after PlanApprovalRequested")
	}
	if stripAnsi(after) != "Approve plan" {
		t.Fatalf("headline = %q, want Approve plan", stripAnsi(after))
	}
}

func TestInputViewUsesPaddedStyledSurfaceWithoutExtraShadowLine(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m.input.SetValue("hello")

	raw := m.inputView()
	plain := stripAnsi(raw)
	lines := strings.Split(plain, "\n")
	if len(lines) != 1 {
		t.Fatalf("inputView lines = %d, want 1; view:\n%q", len(lines), plain)
	}
	if strings.ContainsAny(plain, "┌┐└┘│─") {
		t.Fatalf("input view should not render a border box:\n%s", plain)
	}
	if !strings.HasPrefix(lines[0], " ") || !strings.Contains(lines[0], "hello") {
		t.Fatalf("input content should be horizontally padded:\n%q", lines[0])
	}
	if len([]rune(lines[0])) != m.contentWidth() {
		t.Fatalf("input line width = %d, want %d; line=%q", len([]rune(lines[0])), m.contentWidth(), lines[0])
	}
	if !strings.Contains(raw, "\x1b[") {
		t.Fatalf("input view should contain ANSI styling for the surface: %q", raw)
	}
}

func TestInputLayoutHeightMatchesTextareaHeight(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 18})

	ls := m.measureLayout()
	want := clamp(m.input.Height(), simpleInputMinHeight, simpleInputMaxHeight)
	if ls.inputH != want {
		t.Fatalf("inputH = %d, want textarea height = %d", ls.inputH, want)
	}
}

func TestHeaderBarSeparatedFromViewport(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})

	view := stripAnsi(m.View().Content)
	lines := strings.Split(view, "\n")
	headerIdx := -1
	sepIdx := -1
	viewportIdx := -1
	for i, line := range lines {
		if headerIdx < 0 && strings.Contains(line, "API ✓0") {
			headerIdx = i
			continue
		}
		trimmed := strings.TrimSpace(line)
		if headerIdx >= 0 && sepIdx < 0 && trimmed != "" && strings.Trim(trimmed, "─") == "" {
			sepIdx = i
			continue
		}
		if sepIdx >= 0 && viewportIdx < 0 && strings.Contains(line, "Cece ready. Type a message and press Enter.") {
			viewportIdx = i
			break
		}
	}
	if headerIdx < 0 {
		t.Fatalf("missing header bar in view:\n%s", view)
	}
	if sepIdx != headerIdx+1 {
		t.Fatalf("expected separator immediately after header; headerIdx=%d sepIdx=%d\n%s", headerIdx, sepIdx, view)
	}
	if viewportIdx <= sepIdx {
		t.Fatalf("expected viewport content after separator; sepIdx=%d viewportIdx=%d\n%s", sepIdx, viewportIdx, view)
	}
}

func TestSubAgentRunBarTracksLifecycle(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 12})

	m.ApplyEventForTest(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "Exploring UI"})
	view := stripAnsi(m.agentBarView())
	if !strings.Contains(view, "Agent] Exploring UI") {
		t.Fatalf("running sub-agent label not rendered:\n%s", view)
	}
	if strings.Contains(m.status, "Exploring UI") {
		t.Fatalf("sub-agent start/activity should not duplicate in status: %q", m.status)
	}

	// Activity updates: agent bar still just shows the label line
	m.ApplyEventForTest(protocol.SubAgentActivityEvent{ID: "agent-1", Activity: "Read /tmp/file.go"})
	view = stripAnsi(m.agentBarView())
	if !strings.Contains(view, "Agent] Exploring UI") {
		t.Fatalf("running sub-agent still rendered after activity:\n%s", view)
	}

	m.ApplyEventForTest(protocol.SubAgentActivityEvent{ID: "agent-1", Activity: "Edit /tmp/file.go"})
	view = stripAnsi(m.agentBarView())
	if !strings.Contains(view, "Agent] Exploring UI") {
		t.Fatalf("running sub-agent still rendered after 2nd activity:\n%s", view)
	}

	m.ApplyEventForTest(protocol.SubAgentCompletedEvent{ID: "agent-1", Description: "Exploring UI"})
	view = stripAnsi(m.agentBarView())
	// After completion, agent bar shows ✓ with "done" briefly before TTL purge.
	if !strings.Contains(view, "✓") || !strings.Contains(view, "done") {
		t.Fatalf("completed sub-agent should show ✓ done:\n%s", view)
	}
	// Simulate TTL expiry by setting DoneAt far in the past.
	m.runningAgents[0].DoneAt = time.Now().Add(-agentDoneTTL - time.Second)
	view = stripAnsi(m.agentBarView())
	if strings.Contains(view, "Exploring UI") {
		t.Fatalf("completed sub-agent should be purged after TTL:\n%s", view)
	}
}

func TestCancelTurnClearsRunningSubAgents(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "Exploring UI"})

	m.cancelTurn("Cancelled")

	if len(m.runningAgents) != 0 {
		t.Fatalf("runningAgents len = %d, want 0", len(m.runningAgents))
	}
	view := stripAnsi(m.agentBarView())
	if strings.Contains(view, "Exploring UI") {
		t.Fatalf("cancelled sub-agent still rendered:\n%s", view)
	}
}

// stripAnsi removes ANSI escape sequences from s.
func stripAnsi(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// skip until letter
			for i < len(s) && !(s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
				i++
			}
			i++ // skip the letter
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func TestInitOnlySubscribesEvents(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected event subscription command")
	}
	if len(sender.actions) != 0 {
		t.Fatalf("Init dispatched actions: %#v", sender.actions)
	}
}

func viewLineCountForTest(m *Model) int {
	content := stripAnsi(m.View().Content)
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func TestViewLineCountStableWithQueuedInput(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m.queued = []string{"queued message"}

	before := viewLineCountForTest(&m)
	m.update(statusSpinnerTickMsg{})
	after := viewLineCountForTest(&m)

	if after != before {
		t.Fatalf("view line count changed with queued input: before=%d after=%d", before, after)
	}
}

func TestViewLineCountStableWithRunningAgent(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m.ApplyEventForTest(protocol.SubAgentStartedEvent{ID: "agent-1", Description: "Exploring UI"})

	before := viewLineCountForTest(&m)
	m.update(statusSpinnerTickMsg{})
	after := viewLineCountForTest(&m)

	if after != before {
		t.Fatalf("view line count changed with running agent: before=%d after=%d", before, after)
	}
}

func TestTaskBarHeightIncludesOverflowLine(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tasks = []protocol.TodoItem{
		{Content: "one", ActiveForm: "working one", Status: "in_progress"},
		{Content: "two", Status: "completed"},
		{Content: "three", Status: "completed"},
		{Content: "four", Status: "completed"},
		{Content: "five", Status: "completed"},
		{Content: "six", Status: "completed"},
		{Content: "seven", Status: "pending"},
	}

	view := m.taskBarView()
	got := m.taskBarHeight()
	want := strings.Count(view, "\n") + 1
	if got != want {
		t.Fatalf("taskBarHeight() = %d, want rendered line count %d; view:\n%s", got, want, stripAnsi(view))
	}
	plain := stripAnsi(view)
	if strings.Contains(plain, "[Todo List]") || !strings.Contains(plain, "Todo List") {
		t.Fatalf("todo label should not use brackets; view:\n%s", plain)
	}
	if strings.Contains(plain, "working one") {
		t.Fatalf("todo list should show content, not activeForm:\n%s", plain)
	}
	for _, hidden := range []string{"three", "four", "five", "six"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("todo list should show at most 3 items; %q was visible:\n%s", hidden, plain)
		}
	}
	if !containsAll(plain, "seven", "one", "two", "... +4 more") {
		t.Fatalf("todo list missing expected visible/overflow rows:\n%s", plain)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: textForKey(s), Code: codeForKey(s), Mod: modForKey(s)})
}

func newScrollableModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, "sonnet", "/tmp")
	m.update(tea.WindowSizeMsg{Width: 60, Height: 10})
	for i := 0; i < 12; i++ {
		m.ApplyEventForTest(protocol.UserMessageAdded{Message: protocol.Message{Role: "user", Content: strings.Repeat("old message\n", 3)}})
	}
	if bottom := m.viewport.YOffset(); bottom == 0 {
		t.Fatal("test setup failed: transcript is not scrollable")
	}
	return m
}

func textForKey(s string) string {
	if len([]rune(s)) == 1 {
		return s
	}
	return ""
}

func codeForKey(s string) rune {
	switch s {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "ctrl+up", "alt+up":
		return tea.KeyUp
	case "ctrl+down", "alt+down":
		return tea.KeyDown
	case "pgup":
		return tea.KeyPgUp
	case "pgdown":
		return tea.KeyPgDown
	case "home":
		return tea.KeyHome
	case "end":
		return tea.KeyEnd
	case "tab":
		return tea.KeyTab
	case "shift+tab", "backtab":
		return tea.KeyTab
	case "backspace":
		return tea.KeyBackspace
	case " ":
		return tea.KeySpace
	}
	runes := []rune(s)
	if len(runes) == 1 {
		return runes[0]
	}
	return 0
}

func modForKey(s string) tea.KeyMod {
	switch {
	case strings.HasPrefix(s, "ctrl+"):
		return tea.ModCtrl
	case strings.HasPrefix(s, "alt+"):
		return tea.ModAlt
	case strings.HasPrefix(s, "shift+"):
		return tea.ModShift
	default:
		return 0
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

type fakeSessionStore struct{ sessions []session.Session }

func (f *fakeSessionStore) Create(context.Context, string) (*session.Session, error) { return nil, nil }
func (f *fakeSessionStore) AppendMessage(context.Context, string, json.RawMessage) error {
	return nil
}
func (f *fakeSessionStore) LoadMessages(context.Context, string) ([]json.RawMessage, error) {
	return nil, nil
}
func (f *fakeSessionStore) List(context.Context) ([]session.Session, error)               { return f.sessions, nil }
func (f *fakeSessionStore) Get(context.Context, string) (*session.Session, error)         { return nil, nil }
func (f *fakeSessionStore) Rename(context.Context, string, string) error                  { return nil }
func (f *fakeSessionStore) Delete(context.Context, string) error                          { return nil }
func (f *fakeSessionStore) UpdateMeta(context.Context, string, session.SessionMeta) error { return nil }
func (f *fakeSessionStore) SaveInputHistory(context.Context, string, []string) error      { return nil }

func TestChineseInputGoesDirectlyToTextarea(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	// Simulate a CJK key press from IME
	chineseKey := tea.KeyPressMsg(tea.Key{Code: tea.KeyExtended, Text: "你"})
	model, _ := m.handleKey(chineseKey)
	m2 := model.(*Model)
	if got := m2.input.Value(); got != "你" {
		t.Fatalf("input = %q, want %q", got, "你")
	}

	// Second character
	chineseKey2 := tea.KeyPressMsg(tea.Key{Code: tea.KeyExtended, Text: "好"})
	model2, _ := m2.handleKey(chineseKey2)
	m3 := model2.(*Model)
	if got := m3.input.Value(); got != "你好" {
		t.Fatalf("input = %q, want %q", got, "你好")
	}
}

func TestChineseInputDoesNotTriggerEnter(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	// Type Chinese then press enter — only enter should submit
	chineseKey := tea.KeyPressMsg(tea.Key{Code: tea.KeyExtended, Text: "你"})
	model, _ := m.handleKey(chineseKey)
	m2 := model.(*Model)
	// Now press enter to submit
	_, cmd := m2.handleKey(keyMsg("enter"))
	if cmd != nil {
		cmd()
	}
	if len(sender.inputs) != 1 || sender.inputs[0] != "你" {
		t.Fatalf("inputs = %v, want [你]", sender.inputs)
	}
}

func TestChineseInputInModalTextMode(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{{
		Question: "Pick one",
		Options:  []protocol.QuestionOption{{Label: "A"}, {Label: "B"}},
	}}})
	// Move to "Type something else..." (index 2, which is len(Options))
	m.handleModalKey(keyMsg("down"))
	m.handleModalKey(keyMsg("down"))
	// Enter textMode
	m.handleModalKey(keyMsg("enter"))
	if !m.modal.textMode {
		t.Fatal("expected textMode after enter on 'Type something else...'")
	}
	// Simulate CJK input — should go to modal.textInput, not main input
	chineseKey := tea.KeyPressMsg(tea.Key{Code: tea.KeyExtended, Text: "你好"})
	model, _ := m.handleKey(chineseKey)
	m2 := model.(*Model)
	if m2.modal.textInput != "你好" {
		t.Fatalf("modal.textInput = %q, want %q", m2.modal.textInput, "你好")
	}
	if m2.input.Value() != "" {
		t.Fatalf("main input = %q, want empty", m2.input.Value())
	}
	// Submit with enter
	_, cmd := m2.handleKey(keyMsg("enter"))
	if cmd != nil {
		_ = cmd()
	}
	action, ok := sender.actions[len(sender.actions)-1].(protocol.AnswerQuestionAction)
	if !ok {
		t.Fatalf("last action = %T, want AnswerQuestionAction", sender.actions[len(sender.actions)-1])
	}
	if action.Answers[0].Custom != "你好" {
		t.Fatalf("custom answer = %q, want %q", action.Answers[0].Custom, "你好")
	}
}

func TestPasteInModalTextMode(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.QuestionAsked{Questions: []protocol.Question{{
		Question: "Pick one",
		Options:  []protocol.QuestionOption{{Label: "A"}},
	}}})
	// Move to "Type something else..." (index 1)
	m.handleModalKey(keyMsg("down"))
	m.handleModalKey(keyMsg("enter"))
	if !m.modal.textMode {
		t.Fatal("expected textMode")
	}
	// Paste — should go to modal.textInput, not main input
	m.update(tea.PasteMsg{Content: "pasted text"})
	if m.modal.textInput != "pasted text" {
		t.Fatalf("modal.textInput = %q, want %q", m.modal.textInput, "pasted text")
	}
	if m.input.Value() != "" {
		t.Fatalf("main input = %q, want empty", m.input.Value())
	}
}

func TestIsASCII(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"hello", true},
		{"", true},
		{"A", true},
		{"你", false},
		{"hello你好", false},
		{"!", true},
	}
	for _, tc := range cases {
		if got := isASCII(tc.input); got != tc.want {
			t.Errorf("isASCII(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestModelPasteSanitizesVisibleCSIResidue(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	_, cmd := m.Update(tea.PasteMsg{Content: "line1[27;5;106~line2[27;5;106~"})
	if cmd != nil {
		_ = cmd()
	}
	if got := sanitizePasteContent("line1[27;5;106~line2[27;5;106~"); got != "line1\nline2\n" {
		t.Fatalf("sanitizePasteContent() = %q, want %q", got, "line1\nline2\n")
	}
	if got := m.input.Value(); strings.Contains(got, "[27;5;106~") {
		t.Fatalf("input = %q, want no CSI residue", got)
	}
}

func TestSlashDryRunDispatchesAction(t *testing.T) {
	sender := newRecordingSender()
	m := NewModel(sender, "sonnet", "/tmp")
	m.input.SetValue("/dryrun preview this")
	_, cmd := m.handleKey(keyMsg("enter"))
	if cmd != nil {
		_ = cmd()
	}
	if len(sender.actions) == 0 {
		t.Fatal("expected action")
	}
	action, ok := sender.actions[len(sender.actions)-1].(protocol.DryRunRequestAction)
	if !ok {
		t.Fatalf("last action = %T, want DryRunRequestAction", sender.actions[len(sender.actions)-1])
	}
	if action.Input != "preview this" {
		t.Fatalf("input = %q, want preview this", action.Input)
	}
}

func TestDryRunEventRendersRequestLayers(t *testing.T) {
	m := NewModel(nil, "sonnet", "/tmp")
	m.ApplyEventForTest(protocol.RequestDryRunEvent{
		Input:                "preview",
		MaxTokens:            100,
		EstimatedInputTokens: 42,
		PromptLayers: []protocol.PromptLayerDryRun{{
			Name:          "stable",
			CacheControl:  map[string]string{"type": "ephemeral"},
			TokenEstimate: 2,
			Content:       "system rules",
		}},
		Messages: []protocol.MessageDryRun{{Index: 0, Role: "user", Content: "preview"}},
		Tools:    []protocol.ToolDryRun{{Name: "Read", Description: "read files"}},
	})
	view := m.transcript.render(100, m.styles)
	if !containsAll(view, "Dryrun", "[prompt layers]", "stable", "system rules", "[messages]", "preview", "[tools]", "Read") {
		t.Fatalf("dryrun not rendered:\n%s", view)
	}
	if strings.Contains(view, "[dryrun]") {
		t.Fatalf("dryrun label should not use brackets:\n%s", view)
	}
	if m.transcript.contextUsed != 42 {
		t.Fatalf("contextUsed = %d, want 42", m.transcript.contextUsed)
	}
}
