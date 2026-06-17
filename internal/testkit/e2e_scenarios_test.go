package testkit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/testkit"
	"github.com/zhanglvtao/cece/internal/ui"
)

// ── ConfirmTools modal ─────────────────────────────────────────────────────

func TestE2E_ConfirmTools_Approve(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{})
	llm := testkit.NewScriptedClient(
		// First turn: emit a Write tool call with require_confirmation=true.
		testkit.ToolUseTurn("call-1", "Write",
			`{"file_path":"/tmp/x","content":"hello","require_confirmation":true}`),
		// Second turn (after tool_result): plain reply.
		testkit.TextTurn("done"),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithExtraTools(testkit.NewFakeWrite(fs)),
	)

	h.Send("write a file")

	if !h.WaitForModal("confirm_tools", 5*time.Second) {
		t.Fatalf("confirm_tools modal did not open; status=%q events=%d", h.CurrentUI().StatusForTest(), len(h.EventsSnapshot()))
	}
	h.Confirm()

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if _, ok := fs.Read("/tmp/x"); !ok {
		t.Fatalf("write was not executed after Confirm")
	}
	if h.CurrentUI().ModalActiveForTest() {
		t.Fatalf("modal should be closed after Confirm")
	}
}

func TestE2E_ConfirmTools_Reject(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-1", "Write",
			`{"file_path":"/tmp/x","content":"hello","require_confirmation":true}`),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithExtraTools(testkit.NewFakeWrite(fs)),
	)

	h.Send("write a file")
	if !h.WaitForModal("confirm_tools", 5*time.Second) {
		t.Fatalf("confirm_tools modal did not open")
	}
	h.Reject()

	if !h.WaitForBusy(false, 5*time.Second) {
		t.Fatalf("busy should be false after reject")
	}
	if _, ok := fs.Read("/tmp/x"); ok {
		t.Fatalf("write should NOT have executed after Reject")
	}
}

// ── PlanApproval modal ─────────────────────────────────────────────────────

func TestE2E_PlanApproval_Reject(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-1", "ExitPlanMode", `{"plan_file":"plan.md"}`),
	// synthetic plan preview file must exist under plans dir for approval modal
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithDefaultMode("plan"),
	)
	planDir := filepath.Join(h.Eng.ProjectDir(), ".cece", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan\n\n- Do it\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.Send("plan something")
	if !h.WaitForModal("approve_plan", 5*time.Second) {
		events := h.EventsSnapshot()
		t.Fatalf("approve_plan modal did not open; events=%T", events)
	}
	h.RejectPlan()

	if !h.WaitForBusy(false, 5*time.Second) {
		t.Fatalf("busy should be false after RejectPlan")
	}
	if got := llm.Calls(); got != 1 {
		t.Fatalf("LLM calls after RejectPlan = %d, want 1", got)
	}
	// After reject, mode is set to plan.
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.CurrentUI().ModeForTest() == protocol.PermissionModePlan
	}, true, 1*time.Second) {
		t.Fatalf("mode after reject = %q, want plan", h.CurrentUI().ModeForTest())
	}
}

// ── Slash command path (use h.Do to bypass slash popup) ────────────────────

func TestE2E_Action_ClearHistory(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("ack"))
	h := testkit.NewHarness(t, llm)

	// First send something so transcript is non-empty.
	h.Send("first message")
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	h.Do(protocol.ClearHistoryAction{})
	testkit.WaitForEvent[protocol.HistoryClearedEvent](t, h, nil, 2*time.Second)
}

func TestE2E_Action_ListTools(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Do(protocol.ListToolsAction{})
	testkit.WaitForEvent[protocol.ToolsListedEvent](t, h, nil, 2*time.Second)

	ev, _ := testkit.LastEvent[protocol.ToolsListedEvent](h)
	if len(ev.Tools) == 0 {
		t.Fatalf("ToolsListedEvent.Tools should be non-empty")
	}
}

func TestE2E_Action_DryRun_NoLLMCall(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Do(protocol.DryRunRequestAction{Input: "preview this"})
	testkit.WaitForEvent[protocol.RequestDryRunEvent](t, h, nil, 2*time.Second)

	if llm.Calls() != 0 {
		t.Fatalf("LLM was invoked %d times during dryrun", llm.Calls())
	}
	ev, _ := testkit.LastEvent[protocol.RequestDryRunEvent](h)
	if !strings.Contains(ev.Input, "preview this") {
		t.Fatalf("RequestDryRunEvent.Input = %q", ev.Input)
	}
}

func TestE2E_Action_ListMCP_EmitsServersListed(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm,
		testkit.WithMCPManager(testkit.NewEmptyMCPManager()),
	)

	h.Do(protocol.ListMCPAction{})
	testkit.WaitForEvent[protocol.MCPServersListedEvent](t, h, nil, 2*time.Second)
}

// ── Permission mode ────────────────────────────────────────────────────────

func TestE2E_CycleMode_EmitsModeChanged(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	if got := h.CurrentUI().ModeForTest(); got != protocol.PermissionModeDefault {
		t.Fatalf("initial mode = %q, want default", got)
	}

	h.CycleMode()
	testkit.WaitForEvent[protocol.ModeChangedEvent](t, h, nil, 2*time.Second)
	mode1, _ := testkit.LastEvent[protocol.ModeChangedEvent](h)
	if mode1.Mode == protocol.PermissionModeDefault {
		t.Fatalf("mode after first cycle still default")
	}
}

func TestE2E_SetMode_Direct(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.SetMode(protocol.PermissionModeAutoAccept)
	testkit.WaitForEvent[protocol.ModeChangedEvent](t, h, nil, 2*time.Second)
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.CurrentUI().ModeForTest() == protocol.PermissionModeAutoAccept
	}, true, 2*time.Second) {
		ev, _ := testkit.LastEvent[protocol.ModeChangedEvent](h)
		t.Fatalf("mode = %q, want auto-accept; last ModeChangedEvent.Mode=%q events=%d",
			h.CurrentUI().ModeForTest(), ev.Mode, len(h.EventsSnapshot()))
	}
}

// ── Cancel / Esc / Ctrl+C ──────────────────────────────────────────────────

func TestE2E_CtrlC_BusyCancels(t *testing.T) {
	// Use a turn that never completes naturally; harness will cancel.
	llm := testkit.NewScriptedClient(testkit.TextTurn("hello"))
	h := testkit.NewHarness(t, llm)

	h.Send("anything")
	// Wait for busy=true.
	_ = h.WaitForBusy(true, 1*time.Second)

	// While busy press ctrl+c.
	h.QuitOnce()
	if !h.WaitForBusy(false, 5*time.Second) {
		t.Fatalf("busy should be false after ctrl+c during turn; status=%q", h.CurrentUI().StatusForTest())
	}
}

func TestE2E_CtrlC_NonEmptyClearsInput(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Type("draft")
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }) != ""
	}, true, 1*time.Second) {
		t.Fatalf("input was never populated")
	}
	h.QuitOnce()

	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }) == ""
	}, true, 1*time.Second) {
		t.Fatalf("input should be cleared after ctrl+c on non-empty; got %q",
			h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }))
	}
}

// ── History ────────────────────────────────────────────────────────────────

func TestE2E_HistoryUpRecallsLastInput(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.TextTurn("ok1"),
		testkit.TextTurn("ok2"),
	)
	h := testkit.NewHarness(t, llm)

	h.Send("first")
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)
	h.Send("second")
	testkit.WaitForEventCount[protocol.TurnCompleted](t, h, 2, 5*time.Second)

	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }) == ""
	}, true, 1*time.Second) {
		t.Fatalf("input should be empty after submit; got %q",
			h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }))
	}
	h.Drv.Press("up")
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }) == "second"
	}, true, 1*time.Second) {
		t.Fatalf("input after up = %q",
			h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }))
	}
}

// ── Slash popup ────────────────────────────────────────────────────────────

func TestE2E_SlashPopup_OpensOnSlash(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Press("/")

	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadBool(func(m *ui.Model) bool { return m.SlashPopupActiveForTest() })
	}, true, 1*time.Second) {
		t.Fatalf("slash popup should open after typing /")
	}
	h.Drv.Press("esc")
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadBool(func(m *ui.Model) bool { return m.SlashPopupActiveForTest() })
	}, false, 1*time.Second) {
		t.Fatalf("slash popup should close after esc")
	}
}

// ── Tool execution closed loop with FakeFS ─────────────────────────────────

func TestE2E_ToolUse_WriteThenRead(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-w", "Write",
			`{"file_path":"/p/note.txt","content":"abc"}`),
		testkit.ToolUseTurn("call-r", "Read",
			`{"file_path":"/p/note.txt"}`),
		testkit.TextTurn("done"),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithExtraTools(
			testkit.NewFakeWrite(fs),
			testkit.NewFakeRead(fs),
		),
	)

	h.Send("create then read")
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if got, ok := fs.Read("/p/note.txt"); !ok || got != "abc" {
		t.Fatalf("file content = %q, want abc", got)
	}
	if llm.Calls() != 3 {
		t.Fatalf("LLM should have been called 3 times (tool_use → tool_use → text), got %d", llm.Calls())
	}
}

// ── Window size / paste ────────────────────────────────────────────────────

func TestE2E_Resize_DoesNotPanic(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Resize(40, 10)
	h.Drv.Resize(200, 60)
	// Make View() rendering serialised against the driver's Update.
	var view string
	h.Read(func(m *ui.Model) { view = m.View().Content })
	if view == "" {
		t.Fatalf("empty view after resize")
	}
}

func TestE2E_Paste_AppendsToInput(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Paste("pasted text")
	if !h.Drv.WaitForBoolFn(func() bool {
		return strings.Contains(h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }), "pasted text")
	}, true, 1*time.Second) {
		t.Fatalf("input should contain pasted text; got %q",
			h.ReadString(func(m *ui.Model) string { return m.InputValueForTest() }))
	}
}

// ── Error path ─────────────────────────────────────────────────────────────

func TestE2E_StreamError_RunFailed(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.ErrorTurn(errFake("boom")),
	)
	h := testkit.NewHarness(t, llm)

	h.Send("trigger")
	testkit.WaitForEvent[protocol.RunFailed](t, h, nil, 5*time.Second)
	if !h.WaitForBusy(false, 2*time.Second) {
		t.Fatalf("busy should be false after RunFailed")
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

func agentMsg(role, content string) agent.Message {
	return agent.Message{Role: agent.Role(role), Content: content}
}

// ── ConfirmTools auto-accept via shift+tab ────────────────────────────────

func TestE2E_ConfirmTools_EnableAutoAccept(t *testing.T) {
	fs := testkit.NewFakeFS(map[string]string{})
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("c1", "Write",
			`{"file_path":"/tmp/y","content":"a","require_confirmation":true}`),
		testkit.TextTurn("done"),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithExtraTools(testkit.NewFakeWrite(fs)),
	)

	h.Send("write a file")
	if !h.WaitForModal("confirm_tools", 5*time.Second) {
		t.Fatalf("modal did not open")
	}
	h.AcceptAuto() // shift+tab → SetPermissionModeAction(auto-accept) + Confirm

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.ReadString(func(m *ui.Model) string { return string(m.ModeForTest()) }) == "auto-accept"
	}, true, 1*time.Second) {
		t.Fatalf("mode should be auto-accept after AcceptAuto")
	}
}

// ── PlanApproval Approve ──────────────────────────────────────────────────

func TestE2E_PlanApproval_Approve(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("p1", "ExitPlanMode", `{"plan_file":"plan.md"}`),
		testkit.TextTurn("plan executed"),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithDefaultMode("plan"),
	)
	planDir := filepath.Join(h.Eng.ProjectDir(), ".cece", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan\n\n- Do it\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.Send("propose a plan")
	if !h.WaitForModal("approve_plan", 5*time.Second) {
		events := h.EventsSnapshot()
		t.Fatalf("approve_plan modal did not open; events=%T", events)
	}
	h.ApprovePlan()

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.CurrentUI().ModeForTest() == protocol.PermissionModeDefault
	}, true, 1*time.Second) {
		t.Fatalf("mode after ApprovePlan = %q, want default", h.CurrentUI().ModeForTest())
	}
	if got := llm.Calls(); got != 2 {
		t.Fatalf("LLM calls after ApprovePlan = %d, want 2", got)
	}
}

func TestE2E_PlanApproval_ApproveAuto(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("p1", "ExitPlanMode", `{"plan_file":"plan.md"}`),
		testkit.TextTurn("plan executed"),
	)
	h := testkit.NewHarness(t, llm,
		testkit.WithYolo(false),
		testkit.WithDefaultMode("plan"),
	)
	planDir := filepath.Join(h.Eng.ProjectDir(), ".cece", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.md"), []byte("# Plan\n\n- Do it\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.Send("propose a plan")
	if !h.WaitForModal("approve_plan", 5*time.Second) {
		events := h.EventsSnapshot()
		t.Fatalf("approve_plan modal did not open; events=%T", events)
	}
	h.ApprovePlanAuto()

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.CurrentUI().ModeForTest() == protocol.PermissionModeAutoAccept
	}, true, 1*time.Second) {
		t.Fatalf("mode after ApprovePlanAuto = %q, want auto-accept", h.CurrentUI().ModeForTest())
	}
	if got := llm.Calls(); got != 2 {
		t.Fatalf("LLM calls after ApprovePlanAuto = %d, want 2", got)
	}
}

// ── Esc cancels turn ─────────────────────────────────────────────────────

func TestE2E_Esc_DoesNotCrashWhenIdle(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Cancel() // esc when idle is a no-op
	if h.ReadBool(func(m *ui.Model) bool { return m.BusyForTest() }) {
		t.Fatalf("busy should remain false after esc when idle")
	}
}

// ── Queue input during busy ──────────────────────────────────────────────

func TestE2E_QueueInputDuringBusy(t *testing.T) {
	gate := make(chan struct{})
	llm := testkit.NewScriptedClient(testkit.ScriptedTurn{Text: "first response", Block: gate})
	h := testkit.NewHarness(t, llm)

	h.Send("first")
	if !h.WaitForBusy(true, 2*time.Second) {
		t.Fatalf("turn should be busy")
	}

	h.Send("queued")
	if !h.Drv.WaitForBoolFn(func() bool {
		return len(h.ReadStrings(func(m *ui.Model) []string { return m.QueuedForTest() })) >= 1
	}, true, 2*time.Second) {
		t.Fatalf("expected at least 1 queued input, got %v",
			h.ReadStrings(func(m *ui.Model) []string { return m.QueuedForTest() }))
	}
	close(gate)
}

// ── DeleteSession action ─────────────────────────────────────────────────

func TestE2E_DeleteSession_RemovesFromStore(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("ack"))
	h := testkit.NewHarness(t, llm)

	h.Send("create session")
	testkit.WaitForEvent[protocol.SessionCreated](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	created, _ := testkit.FirstEvent[protocol.SessionCreated](h)
	if created.ID == "" {
		t.Fatalf("SessionCreated.ID empty")
	}

	// Confirm session is in store.
	if _, err := h.Store.Get(context.Background(), created.ID); err != nil {
		t.Fatalf("store.Get before delete: %v", err)
	}

	h.Do(protocol.DeleteSessionAction{SessionID: created.ID})
	testkit.WaitForEvent[protocol.SessionDeletedEvent](t, h, nil, 2*time.Second)

	if _, err := h.Store.Get(context.Background(), created.ID); err == nil {
		t.Fatalf("session should be gone from store after delete")
	}
}

// ── PreloadSession via store, then LoadSession ────────────────────────────

func TestE2E_LoadSession_RestoresFromStore(t *testing.T) {
	store := testkit.NewMemStore()
	sess, _ := store.Create(context.Background(), "preloaded")
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm, testkit.WithStore(store))

	h.LoadSession(sess.ID)
	testkit.WaitForEvent[protocol.SessionLoadedEvent](t, h, nil, 2*time.Second)
	loaded, _ := testkit.LastEvent[protocol.SessionLoadedEvent](h)
	if loaded.SessionID != sess.ID {
		t.Fatalf("loaded session id = %q, want %q", loaded.SessionID, sess.ID)
	}
}

// ── ListModels ────────────────────────────────────────────────────────────

func TestE2E_ListModels_EmitsModelsLoaded(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Do(protocol.ListModelsAction{})
	testkit.WaitForEvent[protocol.ModelsLoadedEvent](t, h, nil, 2*time.Second)
	ev, _ := testkit.LastEvent[protocol.ModelsLoadedEvent](h)
	if len(ev.Models) == 0 {
		t.Fatalf("ModelsLoadedEvent.Models should be non-empty")
	}
}

// ── AnswerQuestion modal flow ────────────────────────────────────────────
// Note: in default mode AskUserQuestion auto-approves; in plan mode it
// also auto-approves because the InteractionGate treats mode-effect
// tools as read-only. Triggering the question modal requires either
// auto-accept-off + a non-default non-plan custom code path, or
// explicit require_confirmation in the tool input. Skip for now.
//
// func TestE2E_QuestionModal_AnswersQuestion(t *testing.T) { ... }

// ── Compaction action ────────────────────────────────────────────────────

func TestE2E_Compact_EmitsEvents(t *testing.T) {
	// Compact requires the LLM to produce a summary; provide a turn.
	llm := testkit.NewScriptedClient(testkit.TextTurn("summary text"))
	h := testkit.NewHarness(t, llm)

	// Need at least one user message to trigger meaningful compact.
	h.Eng.AppendHistory(agentMsg("user", "first user"))
	h.Eng.AppendHistory(agentMsg("assistant", "first assistant"))

	h.Do(protocol.CompactAction{})
	testkit.WaitForEvent[protocol.CompactingEvent](t, h, nil, 2*time.Second)
	testkit.WaitForEvent[protocol.CompactedEvent](t, h, nil, 5*time.Second)
}
