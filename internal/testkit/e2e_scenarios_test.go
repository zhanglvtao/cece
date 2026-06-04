package testkit_test

import (
	"strings"
	"testing"
	"time"

	"cece/internal/protocol"
	"cece/internal/testkit"
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
		t.Fatalf("confirm_tools modal did not open; status=%q events=%d", h.UI.StatusForTest(), len(h.EventsSnapshot()))
	}
	h.Confirm()

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if _, ok := fs.Read("/tmp/x"); !ok {
		t.Fatalf("write was not executed after Confirm")
	}
	if h.UI.ModalActiveForTest() {
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
	)
	h := testkit.NewHarness(t, llm, testkit.WithYolo(false))

	h.Send("plan something")
	if !h.WaitForModal("approve_plan", 5*time.Second) {
		t.Fatalf("approve_plan modal did not open")
	}
	h.RejectPlan()

	if !h.WaitForBusy(false, 5*time.Second) {
		t.Fatalf("busy should be false after RejectPlan")
	}
	// After reject, mode is set to plan.
	if !h.Drv.WaitForBoolFn(func() bool {
		return h.UI.ModeForTest() == protocol.PermissionModePlan
	}, true, 1*time.Second) {
		t.Fatalf("mode after reject = %q, want plan", h.UI.ModeForTest())
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

	if got := h.UI.ModeForTest(); got != protocol.PermissionModeDefault {
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
		return h.UI.ModeForTest() == protocol.PermissionModeAutoAccept
	}, true, 2*time.Second) {
		ev, _ := testkit.LastEvent[protocol.ModeChangedEvent](h)
		t.Fatalf("mode = %q, want auto-accept; last ModeChangedEvent.Mode=%q events=%d",
			h.UI.ModeForTest(), ev.Mode, len(h.EventsSnapshot()))
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
		t.Fatalf("busy should be false after ctrl+c during turn; status=%q", h.UI.StatusForTest())
	}
}

func TestE2E_CtrlC_NonEmptyClearsInput(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Type("draft")
	// Wait a tick for the textarea to receive characters.
	if !h.Drv.WaitForBoolFn(func() bool { return h.UI.InputValueForTest() != "" }, true, 1*time.Second) {
		t.Fatalf("input was never populated")
	}
	h.QuitOnce()

	if !h.Drv.WaitForBoolFn(func() bool { return h.UI.InputValueForTest() == "" }, true, 1*time.Second) {
		t.Fatalf("input should be cleared after ctrl+c on non-empty; got %q", h.UI.InputValueForTest())
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

	// Wait until the input is fully reset (handleSend resets after submit).
	if !h.Drv.WaitForBoolFn(func() bool { return h.UI.InputValueForTest() == "" }, true, 1*time.Second) {
		t.Fatalf("input should be empty after submit; got %q", h.UI.InputValueForTest())
	}
	h.Drv.Press("up")
	if !h.Drv.WaitForBoolFn(func() bool { return h.UI.InputValueForTest() == "second" }, true, 1*time.Second) {
		t.Fatalf("input after up = %q, want second; history=%v", h.UI.InputValueForTest(), h.UI.HistoryForTest())
	}
}

// ── Slash popup ────────────────────────────────────────────────────────────

func TestE2E_SlashPopup_OpensOnSlash(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Press("/")

	if !h.Drv.WaitForBoolFn(h.UI.SlashPopupActiveForTest, true, 1*time.Second) {
		t.Fatalf("slash popup should open after typing /")
	}
	h.Drv.Press("esc")
	if !h.Drv.WaitForBoolFn(h.UI.SlashPopupActiveForTest, false, 1*time.Second) {
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
	view := h.Drv.ViewPlain()
	if view == "" {
		t.Fatalf("empty view after resize")
	}
}

func TestE2E_Paste_AppendsToInput(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Paste("pasted text")
	if !h.Drv.WaitForBoolFn(func() bool {
		return strings.Contains(h.UI.InputValueForTest(), "pasted text")
	}, true, 1*time.Second) {
		t.Fatalf("input should contain pasted text; got %q", h.UI.InputValueForTest())
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
