package recording

import (
	"testing"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/testkit"
	"github.com/zhanglvtao/cece/internal/tool"
)

func TestCassette_Roundtrip(t *testing.T) {
	// Create a known cassette
	original := &Cassette{
		Turns: []CassetteTurn{
			{
				InputTokens:  100,
				OutputTokens: 50,
				Events: []CassetteEvent{
					{EventType: "message_start", InputTokens: 100},
					{Delta: "hello", Detail: "text_delta"},
					{EventType: "message_delta", OutputTokens: 50, StopReason: "end_turn"},
					{Done: true, EventType: "message_stop"},
				},
			},
		},
	}

	// Save and reload
	path := t.TempDir() + "/test.cassette.json"
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(loaded.Turns))
	}
	if loaded.Turns[0].InputTokens != 100 {
		t.Fatalf("inputTokens = %d, want 100", loaded.Turns[0].InputTokens)
	}
	if len(loaded.Turns[0].Events) != 4 {
		t.Fatalf("events = %d, want 4", len(loaded.Turns[0].Events))
	}
}

func TestReplayClient_ReplaysCassette(t *testing.T) {
	c := &Cassette{
		Turns: []CassetteTurn{
			{
				Events: []CassetteEvent{
					{EventType: "message_start", InputTokens: 8},
					{Delta: "hello world", Detail: "text_delta"},
					{Done: true, EventType: "message_stop"},
				},
			},
		},
	}

	client := NewReplayClient(c)
	ch, err := client.Stream(nil, nil, agent.SystemPrompt{}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var events []agent.ApiStreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if events[1].Delta != "hello world" {
		t.Fatalf("delta = %q, want %q", events[1].Delta, "hello world")
	}
}

func TestRecordingClient_CapturesEvents(t *testing.T) {
	// Inner client: use a scripted client
	inner := testkit.NewScriptedClient(testkit.TextTurn("recorded"))
	rec := NewRecordingClient(inner)

	ch, err := rec.Stream(nil, nil, agent.SystemPrompt{}, []tool.Definition{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Drain
	for range ch {
	}

	cassette := rec.Cassette()
	if len(cassette.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(cassette.Turns))
	}
	// ScriptedClient TextTurn produces 4 events (message_start, content_block_start, text_delta, message_delta, message_stop)
	if len(cassette.Turns[0].Events) < 3 {
		t.Fatalf("events = %d, want >= 3", len(cassette.Turns[0].Events))
	}
}