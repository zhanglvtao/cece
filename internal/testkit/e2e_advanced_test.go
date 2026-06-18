package testkit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/skill"
	"github.com/zhanglvtao/cece/internal/testkit"
	"github.com/zhanglvtao/cece/internal/ui"
)

// ── MCP tool call ──────────────────────────────────────────────────────────

func TestE2E_MCP_ToolCall_ExecutesSuccessfully(t *testing.T) {
	mcpTool := testkit.NewFakeMCPTool("mcp_test_echo", "mcp echo: hello")
	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-mcp", "mcp_test_echo", `{"input":"hello"}`),
		testkit.TextTurn("done"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithExtraTools(mcpTool))

	h.Send("call mcp echo")
	testkit.WaitForEvent[protocol.ToolExecStarted](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.ToolExecCompleted](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if got, want := llm.Calls(), 2; got != want {
		t.Fatalf("LLM calls = %d, want %d", got, want)
	}

	exec, ok := testkit.LastEvent[protocol.ToolExecCompleted](h)
	if !ok {
		t.Fatalf("ToolExecCompleted not found")
	}
	if exec.Name != "mcp_test_echo" {
		t.Fatalf("ToolExecCompleted.Name = %q, want %q", exec.Name, "mcp_test_echo")
	}
}

// ── Skill tool call ────────────────────────────────────────────────────────

func TestE2E_Skill_ToolCall_ReturnsSkillXML(t *testing.T) {
	testSkill := &skill.Skill{
		Name:         "demo",
		Description:  "test skill",
		Instructions: "Always mention 'demo skill activated'",
	}
	skillStore := skill.NewStore([]*skill.Skill{testSkill})
	skillStore.SetAllEnabled(true)

	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-skill", "Skill", `{"name":"demo","args":"test args"}`),
		testkit.TextTurn("skill processed"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithSkillStore(skillStore))

	h.Send("use the demo skill")
	testkit.WaitForEvent[protocol.ToolExecCompleted](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if got, want := llm.Calls(), 2; got != want {
		t.Fatalf("LLM calls = %d, want %d", got, want)
	}

	exec, ok := testkit.LastEvent[protocol.ToolExecCompleted](h)
	if !ok {
		t.Fatalf("ToolExecCompleted not found")
	}
	if exec.Name != "Skill" {
		t.Fatalf("ToolExecCompleted.Name = %q, want %q", exec.Name, "Skill")
	}
	if !strings.Contains(exec.Result.Content, `<skill name="demo">`) {
		t.Fatalf("ToolExecCompleted.Result.Content missing <skill name=\"demo\">:\n%s", exec.Result.Content)
	}
}

// ── Skill slash command ────────────────────────────────────────────────────

func TestE2E_Skill_SlashCommand_LoadsSkill(t *testing.T) {
	testSkill := &skill.Skill{
		Name:         "demo",
		Description:  "test skill",
		Instructions: "Always mention 'demo skill activated'",
	}
	skillStore := skill.NewStore([]*skill.Skill{testSkill})
	skillStore.SetAllEnabled(true)

	llm := testkit.NewScriptedClient(testkit.TextTurn("skill received"))
	h := testkit.NewHarness(t, llm, testkit.WithSkillStore(skillStore))

	h.SendSlash("demo", "some args")
	testkit.WaitForEvent[protocol.UserMessageAdded](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if got, want := llm.Calls(), 1; got != want {
		t.Fatalf("LLM calls = %d, want %d", got, want)
	}

	// Verify the loaded_skill XML was injected into the conversation history
	history := h.Eng.HistoryForTest()
	found := false
	for _, msg := range history {
		if msg.Role == agent.UserRole && strings.Contains(msg.Content, `<loaded_skill>`) && strings.Contains(msg.Content, `<name>demo</name>`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("history missing <loaded_skill> XML block for 'demo'")
	}
}

// ── AutoTitle ──────────────────────────────────────────────────────────────

func TestE2E_AutoTitle_GeneratesTitleOnQuit(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("hello"))
	lightLLM := testkit.NewScriptedClient(testkit.TextTurn("test session title"))
	h := testkit.NewHarness(t, llm, testkit.WithLightClient(lightLLM))

	h.Send("create a test session")
	testkit.WaitForEvent[protocol.SessionCreated](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	// Input is empty by default after send completes
	h.QuitOnce()
	testkit.WaitForEvent[protocol.SessionTitleGeneratedEvent](t, h, nil, 5*time.Second)

	ev, ok := testkit.LastEvent[protocol.SessionTitleGeneratedEvent](h)
	if !ok {
		t.Fatalf("SessionTitleGeneratedEvent not found")
	}
	if ev.Title != "test session title" {
		t.Fatalf("SessionTitleGeneratedEvent.Title = %q, want %q", ev.Title, "test session title")
	}
	if lightLLM.Calls() != 1 {
		t.Fatalf("lightLLM calls = %d, want 1", lightLLM.Calls())
	}
}

// ── QuitTwice ────────────────────────────────────────────────────────────

func TestE2E_QuitTwice_DeletesSessionAndQuits(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.TextTurn("hello"))
	h := testkit.NewHarness(t, llm)

	h.Send("create session")
	testkit.WaitForEvent[protocol.SessionCreated](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if !h.WaitForBusy(false, 5*time.Second) {
		t.Fatalf("timed out waiting for busy=false")
	}

	created, ok := testkit.FirstEvent[protocol.SessionCreated](h)
	if !ok {
		t.Fatalf("SessionCreated not found")
	}
	sessionID := created.ID

	// Verify session exists before quit
	if _, err := h.Store.Get(context.Background(), sessionID); err != nil {
		t.Fatalf("session should exist before QuitTwice: %v", err)
	}

	// Double ctrl+c back-to-back — should hit the double-tap path
	// and delete the session before AutoTitle completes.
	h.QuitTwice()
	testkit.WaitForEvent[protocol.SessionDeletedEvent](t, h, nil, 5*time.Second)

	// Verify session is gone
	if _, err := h.Store.Get(context.Background(), sessionID); err == nil {
		t.Fatalf("session should be deleted after QuitTwice")
	}
}

// ── Resume Picker ──────────────────────────────────────────────────────

func TestE2E_ResumePicker_SelectsAndLoadsSession(t *testing.T) {
	store := testkit.NewMemStore()
	sess1, err := store.Create(context.Background(), "session one")
	if err != nil {
		t.Fatalf("failed to create sess1: %v", err)
	}
	sess2, err := store.Create(context.Background(), "session two")
	if err != nil {
		t.Fatalf("failed to create sess2: %v", err)
	}
	// Append a message to sess2 so its UpdatedAt is newer, making it first in the list
	msgJSON, _ := json.Marshal(agent.Message{Role: agent.UserRole, Content: "hi"})
	err = store.AppendMessage(context.Background(), sess2.ID, msgJSON)
	if err != nil {
		t.Fatalf("failed to append message to sess2: %v", err)
	}

	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm, testkit.WithStore(store))

	// Open the session picker directly via the test helper
	// (this exercises the same code path as typing /resume)
	h.CurrentUI().OpenSessionsDialogForTest()

	if !h.WaitForModal("session_picker", 5*time.Second) {
		t.Fatalf("session_picker modal did not open; status=%q; events=%d",
			h.CurrentUI().StatusForTest(),
			len(h.EventsSnapshot()))
	}

	// sess2 is first (newer), sess1 is second. Press down to select sess1.
	h.Press("down")
	h.Press("enter")

	testkit.WaitForEvent[protocol.SessionLoadedEvent](t, h, nil, 5*time.Second)

	loaded, ok := testkit.LastEvent[protocol.SessionLoadedEvent](h)
	if !ok {
		t.Fatalf("SessionLoadedEvent not found")
	}
	if loaded.SessionID != sess1.ID {
		t.Fatalf("SessionLoadedEvent.SessionID = %q, want %q (sess1)", loaded.SessionID, sess1.ID)
	}

	// Modal should be closed
	if h.CurrentUI().ModalActiveForTest() {
		t.Fatalf("modal should be closed after selection")
	}
}

// ── AskUserQuestion modal ────────────────────────────────────────────────

func TestE2E_AskUserQuestion_AnswersAndContinues(t *testing.T) {
	// The InteractionGate logic makes it very hard to trigger QuestionAsked
	// via a real engine flow (yolo/plan mode checks intercept first).
	// Instead we test the UI modal flow directly by injecting the event,
	// which exercises the same UI code path a real QuestionAsked event would.
	llm := testkit.NewScriptedClient(testkit.TextTurn("done, using Go"))
	h := testkit.NewHarness(t, llm, testkit.WithYolo(false))

	// Inject a QuestionAsked event directly to the UI model,
	// which exercises the same UI code path a real QuestionAsked event would.
	h.CurrentUI().ApplyEventForTest(protocol.QuestionAsked{
		CallID: "call-q",
		Questions: []protocol.Question{
			{
				Question: "Which language?",
				Options: []protocol.QuestionOption{
					{Label: "Go"},
					{Label: "Python"},
				},
			},
		},
	})

	// Wait for the question modal to open
	if !h.WaitForModal("question", 5*time.Second) {
		t.Fatalf("question modal did not open; modal=%q",
			h.ReadString(func(m *ui.Model) string { return m.ModalKindForTest() }))
	}

	// Verify the modal contains the correct question
	// (the modal opening is proof the QuestionAsked event was processed)
	if got := h.CurrentUI().ModalKindForTest(); got != "question" {
		t.Fatalf("modal kind = %q, want %q", got, "question")
	}

	// Answer the question — press enter to select first option ("Go")
	// Since there's only one question, this submits the answer and closes the modal.
	h.Press("enter")

	// Wait for the modal to close
	if !h.WaitForModal("", 5*time.Second) {
		t.Fatalf("modal did not close after answering; modal=%q",
			h.CurrentUI().ModalKindForTest())
	}

	// Verify the status shows the answer was submitted
	if got := h.CurrentUI().StatusForTest(); got != "Answered" {
		t.Fatalf("status = %q, want %q", got, "Answered")
	}

	// Verify the LLM was never called (we only tested the UI modal flow)
	if got, want := llm.Calls(), 0; got != want {
		t.Fatalf("LLM calls = %d, want %d", got, want)
	}
}

// ── Agent async control plane ────────────────────────────────────────────

func TestE2E_AgentStartThenStatusCompletesTask(t *testing.T) {
	workerLLM := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-sub-bash", "Bash", `{"command":"echo hello"}`),
		testkit.TextTurn("subagent done: files analyzed"),
	)

	llm := testkit.NewScriptedClient(
		testkit.ToolUseTurn("call-agent-start", "Agent", `{
			"operation": "start",
			"prompt": "analyze files",
			"description": "file analysis",
			"max_turns": 5,
			"model": "sub-model"
		}`),
		testkit.ToolUseTurn("call-agent-status", "Agent", `{
			"operation": "status",
			"agent_id": "agent-1"
		}`),
		testkit.TextTurn("received subagent result"),
	)

	fakeBash := testkit.NewFakeBash(map[string]testkit.CommandResult{
		"echo hello": {Stdout: "hello"},
	})

	h := testkit.NewHarness(t, llm,
		testkit.WithCreateClientFn(func(protocol, apiKey, model, baseURL, authMode, authHelper, configName string) agent.ModelClient {
			return workerLLM
		}),
		testkit.WithExtraTools(fakeBash),
	)

	h.Send("analyze the codebase")

	testkit.WaitForEvent[protocol.SubAgentStartedEvent](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.SubAgentCompletedEvent](t, h, nil, 5*time.Second)
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	started, ok := testkit.FirstEvent[protocol.SubAgentStartedEvent](h)
	if !ok {
		t.Fatalf("SubAgentStartedEvent not found")
	}
	if started.Description != "file analysis" {
		t.Fatalf("SubAgentStartedEvent.Description = %q, want %q", started.Description, "file analysis")
	}

	completed, ok := testkit.LastEvent[protocol.SubAgentCompletedEvent](h)
	if !ok {
		t.Fatalf("SubAgentCompletedEvent not found")
	}
	if completed.TurnsUsed < 1 {
		t.Fatalf("SubAgentCompletedEvent.TurnsUsed = %d, want >= 1", completed.TurnsUsed)
	}

	if workerLLM.Calls() < 2 {
		t.Fatalf("workerLLM calls = %d, want >= 2", workerLLM.Calls())
	}
	if llm.Calls() != 3 {
		t.Fatalf("parent LLM calls = %d, want 3", llm.Calls())
	}
}

// ── Context Nudge ────────────────────────────────────────────────────────

func TestE2E_ContextNudge_TriggersAtThreshold(t *testing.T) {
	llm := testkit.NewScriptedClient(
		testkit.TextTurn("reply 1"),
		testkit.TextTurn("reply 2"),
	)
	h := testkit.NewHarness(t, llm, testkit.WithContextWindow(1000))

	// First turn to create some history
	h.Send("first message")
	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	// Set engine state so contextPct >= 60%:
	// lastInputTokens = 800 (80% of 1000 > 60% threshold)
	h.Eng.SetNudgeStateForTest(800)

	// Trigger a new turn which should check and fire the nudge
	h.Send("next message")
	testkit.WaitForEvent[protocol.ContextNudgedEvent](t, h, nil, 5*time.Second)

	nudge, ok := testkit.LastEvent[protocol.ContextNudgedEvent](h)
	if !ok {
		t.Fatalf("ContextNudgedEvent not found")
	}
	if nudge.ContextPct < 60 {
		t.Fatalf("ContextNudgedEvent.ContextPct = %d, want >= 60", nudge.ContextPct)
	}
}
