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
	if !containsAll(view, "[you]", "hi", "cece", "hello there") {
		t.Fatalf("transcript missing expected content:\n%s", view)
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
	if !containsAll(rendered, "[Grep]", "estimated input: 80981", "tool results: Grep") {
		t.Fatalf("tool line missing request summary:\n%s", rendered)
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
	if parts[0] != "plan ✎" {
		t.Fatalf("default mode statusbar column = %q, want %q", parts[0], "plan ✎")
	}

	m.ApplyEventForTest(protocol.ModeChangedEvent{Mode: protocol.PermissionModeAutoAccept, Message: "Auto-accept mode"})
	got = stripAnsi(m.statusBar.Render(120))
	parts = strings.Split(got, " | ")
	if parts[0] != "auto-accept ✓" {
		t.Fatalf("changed mode statusbar column = %q, want %q", parts[0], "auto-accept ✓")
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
		if headerIdx >= 0 && sepIdx < 0 && strings.TrimSpace(line) != "" && strings.Trim(line, "─") == "" {
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
		{Content: "one", Status: "completed"},
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
	if !containsAll(view, "[dryrun]", "[prompt layers]", "stable", "system rules", "[messages]", "preview", "[tools]", "Read") {
		t.Fatalf("dryrun not rendered:\n%s", view)
	}
	if m.transcript.contextUsed != 42 {
		t.Fatalf("contextUsed = %d, want 42", m.transcript.contextUsed)
	}
}
