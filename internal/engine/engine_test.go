package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cece/internal/chat"
	"cece/internal/prompt"
	"cece/internal/protocol"
	"cece/internal/tool"
)

type fakeClient struct {
	chunks    []chat.ApiStreamEvent
	maxTokens int
}

func (f *fakeClient) Stream(_ context.Context, _ []chat.Message, _ chat.SystemPrompt, _ []tool.Definition, maxTokens int) (<-chan chat.ApiStreamEvent, error) {
	f.maxTokens = maxTokens
	out := make(chan chat.ApiStreamEvent, len(f.chunks))
	for _, chunk := range f.chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func TestNewEngineCreatesWithDefaults(t *testing.T) {
	registry := tool.NewRegistry()
	assembler := prompt.NewContextAssembler("test", registry, nil)
	eng := NewEngine(&fakeClient{}, registry, false, 16384, assembler, "/tmp")

	if eng == nil {
		t.Fatal("NewEngine returned nil")
	}
	if eng.MaxTokens() != 16384 {
		t.Fatalf("MaxTokens = %d, want 16384", eng.MaxTokens())
	}
	if eng.Yolo() != false {
		t.Fatal("Yolo should default to false")
	}
}

func TestEngineModeCycles(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	if eng.Mode() != protocol.PermissionModeDefault {
		t.Fatalf("initial mode = %q, want default", eng.Mode())
	}
}

func TestEngineSetPermissionModeAction(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.Do(protocol.SetPermissionModeAction{Mode: protocol.PermissionModeAutoAccept})
	if eng.Mode() != protocol.PermissionModeAutoAccept {
		t.Fatalf("mode = %q, want auto-accept", eng.Mode())
	}
}

func TestEngineDoDispatchesActions(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	// ConfirmAction should not panic
	eng.Do(protocol.ConfirmAction{})
	eng.Do(protocol.CancelAction{})
	eng.Do(protocol.ClearHistoryAction{})
	eng.Do(protocol.QueueInputAction{Text: "test"})

	// B-class actions should be no-ops on bare Engine (handled by mediator)
	eng.Do(protocol.SwitchModelAction{Model: "test"})
	eng.Do(protocol.ListModelsAction{})
	eng.Do(protocol.CyclePermissionModeAction{})

	if eng.QueuedInputCount() != 1 {
		t.Fatalf("QueuedInputCount = %d, want 1", eng.QueuedInputCount())
	}
	eng.ClearQueuedInputs()
	if eng.QueuedInputCount() != 0 {
		t.Fatal("ClearQueuedInputs did not clear")
	}
}

func TestEngineInputValidation(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	err := eng.Input(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	err = eng.Input(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only input")
	}
}

func TestEngineEventsChannel(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	ch := eng.Events()
	if ch == nil {
		t.Fatal("Events() returned nil channel")
	}

	// EmitEvent should not block
	eng.EmitEvent(protocol.HistoryClearedEvent{})
}

func TestEngineClearHistory(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	eng.ClearHistory()

	history := eng.History()
	if len(history) != 0 {
		t.Fatalf("History after clear = %d, want 0", len(history))
	}
}

func TestEngineHistoryRoundTrip(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	// Load some history
	msgs := []chat.Message{
		{Role: chat.UserRole, Content: "hello"},
		{Role: chat.AssistantRole, Content: "hi there"},
	}
	eng.LoadHistory(context.Background(), "test-session", msgs)

	if eng.SessionID() != "test-session" {
		t.Fatalf("SessionID = %q, want test-session", eng.SessionID())
	}
	history := eng.History()
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Role != string(chat.UserRole) {
		t.Fatalf("history[0].Role = %q, want user", history[0].Role)
	}
}

func TestEngineTurnEngineInterface(t *testing.T) {
	// Verify Engine satisfies chat.TurnEngine at compile time
	var _ chat.TurnEngine = (*Engine)(nil)

	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")

	if eng.ProjectDir() != "/tmp" {
		t.Fatalf("ProjectDir = %q, want /tmp", eng.ProjectDir())
	}
	if eng.HistoryLen() != 0 {
		t.Fatalf("HistoryLen = %d, want 0", eng.HistoryLen())
	}
}

// ── tool stubs for tool execution tests ────────────────────────────────────

type stubTool struct{}

func (stubTool) Info() tool.Definition {
	return tool.Definition{Name: "Stub", Description: "stub", InputSchema: map[string]any{"type": "object"}}
}
func (stubTool) Run(_ context.Context, _ json.RawMessage, _ tool.Emitter) tool.Result {
	return tool.Result{Content: "ok"}
}

func TestEngineDoAnswerQuestion(t *testing.T) {
	eng := NewEngine(&fakeClient{}, tool.NewRegistry(), false, 16384, nil, "/tmp")
	answers := []protocol.QuestionAnswer{
		{Question: "q1", Selected: []string{"a"}},
	}
	// Should not panic
	eng.Do(protocol.AnswerQuestionAction{Answers: answers})
}

// Ensure chat types available
var _ = errors.New
