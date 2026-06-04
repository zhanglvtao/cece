package testkit_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"cece/internal/agent"
	"cece/internal/protocol"
	"cece/internal/testkit"
)

// TestE2E_TypeAndEnter_RunsTurn drives the UI just like a user would:
// type characters into the input box, press enter, and wait for the
// agent loop to complete. Asserts that the transcript contains both
// the user input and the assistant reply, and that the UI returns to
// an idle state.
func TestE2E_TypeAndEnter_RunsTurn(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.ScriptedTurn{Text: "hi there"})
	h := testkit.NewHarness(t, llm)

	h.Drv.Type("hello")
	h.Drv.Press("enter")

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	if got, want := llm.Calls(), 1; got != want {
		t.Fatalf("LLM Stream calls = %d, want %d", got, want)
	}

	view := h.Drv.ViewPlain()
	if !strings.Contains(view, "hello") {
		t.Fatalf("view missing user input %q:\n%s", "hello", view)
	}
	if !strings.Contains(view, "hi there") {
		t.Fatalf("view missing assistant reply %q:\n%s", "hi there", view)
	}

	testkit.WaitForCondition(t, func() bool {
		return strings.Contains(h.Drv.ViewPlain(), "Ready")
	}, 2*time.Second, "status returns to Ready")
}

// TestE2E_FakeLLM_StreamsDeltas verifies multi-delta streaming is
// reassembled into the transcript verbatim.
func TestE2E_FakeLLM_StreamsDeltas(t *testing.T) {
	llm := testkit.NewScriptedClient(testkit.ScriptedTurn{
		Events: []agent.ApiStreamEvent{
			{EventType: "message_start", InputTokens: 10},
			{EventType: "content_block_start", Index: 0, Detail: "text"},
			{Delta: "foo ", Detail: "text_delta"},
			{Delta: "bar", Detail: "text_delta"},
			{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 5},
			{Done: true, EventType: "message_stop"},
		},
	})
	h := testkit.NewHarness(t, llm)

	h.Send("ping")

	testkit.WaitForEvent[protocol.TurnCompleted](t, h, nil, 5*time.Second)

	view := h.Drv.ViewPlain()
	if !strings.Contains(view, "foo bar") {
		t.Fatalf("view missing concatenated deltas %q:\n%s", "foo bar", view)
	}
}

// TestE2E_EmptyEnter_NoOp pressing enter with empty input must not
// trigger a stream call or emit any events.
func TestE2E_EmptyEnter_NoOp(t *testing.T) {
	llm := testkit.NewScriptedClient()
	h := testkit.NewHarness(t, llm)

	h.Drv.Press("enter")

	time.Sleep(50 * time.Millisecond)

	if got := llm.Calls(); got != 0 {
		t.Fatalf("LLM Stream calls = %d, want 0", got)
	}
	if events := h.EventsSnapshot(); len(events) != 0 {
		t.Fatalf("expected no events, got %d (%v)", len(events), eventTypes(events))
	}
}

func eventTypes(events []protocol.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = reflect.TypeOf(ev).String()
	}
	return out
}
