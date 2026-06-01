package ipc

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"cece/internal/protocol"
)

type fakeRuntime struct {
	mu      sync.Mutex
	inputs  []string
	actions []protocol.Action
	events  chan protocol.Event
}

func newFakeRuntime() *fakeRuntime { return &fakeRuntime{events: make(chan protocol.Event, 8)} }
func (f *fakeRuntime) Input(_ context.Context, input string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, input)
	return nil
}
func (f *fakeRuntime) Do(action protocol.Action) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
}
func (f *fakeRuntime) Events() <-chan protocol.Event { return f.events }

func TestServeDispatchesInputAndAction(t *testing.T) {
	r := newFakeRuntime()
	inputLine, _ := MarshalAction(protocol.InputAction{Text: "hi"})
	confirmLine, _ := MarshalAction(protocol.ConfirmAction{})
	stdin := strings.NewReader(string(inputLine) + "\n" + string(confirmLine) + "\n")
	var stdout bytes.Buffer
	if err := Serve(context.Background(), r, stdin, &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(r.inputs) != 1 || r.inputs[0] != "hi" {
		t.Fatalf("inputs = %#v, want [hi]", r.inputs)
	}
	if len(r.actions) != 1 {
		t.Fatalf("actions len = %d, want 1", len(r.actions))
	}
	if _, ok := r.actions[0].(protocol.ConfirmAction); !ok {
		t.Fatalf("action = %T, want ConfirmAction", r.actions[0])
	}
}

func TestServeWritesEvents(t *testing.T) {
	r := newFakeRuntime()
	r.events <- protocol.AssistantDelta{Text: "hello"}
	close(r.events)
	var stdout bytes.Buffer
	if err := Serve(context.Background(), r, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if !strings.Contains(stdout.String(), `"type":"event"`) || !strings.Contains(stdout.String(), `"kind":"assistant_delta"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
