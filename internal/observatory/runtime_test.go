package observatory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

type fakeRuntime struct {
	inputs  []string
	actions []protocol.Action
	events  chan protocol.Event
	mu      sync.Mutex
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{events: make(chan protocol.Event, 8)}
}

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
func (f *fakeRuntime) Wait()                         {}

func TestEventTapRuntimeEmitSendsSyntheticEventToHubAndStream(t *testing.T) {
	base := newFakeRuntime()
	hub := NewHub()
	tap := NewEventTapRuntime(base, hub)
	tap.Emit(protocol.ObservatoryServerStartedEvent{URL: "http://127.0.0.1:1", Host: "127.0.0.1", Port: 1})
	select {
	case ev := <-tap.Events():
		if _, ok := ev.(protocol.ObservatoryServerStartedEvent); !ok {
			t.Fatalf("event = %T, want ObservatoryServerStartedEvent", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for synthetic event")
	}
	if hub.State().Server.URL != "http://127.0.0.1:1" {
		t.Fatalf("hub server URL = %q", hub.State().Server.URL)
	}
}

func TestEventTapRuntimeDelegatesInputDoAndCopiesEvents(t *testing.T) {
	base := newFakeRuntime()
	hub := NewHub()
	tap := NewEventTapRuntime(base, hub)
	if err := tap.Input(context.Background(), "hello"); err != nil {
		t.Fatalf("Input error = %v", err)
	}
	tap.Do(protocol.CancelAction{})
	base.mu.Lock()
	if len(base.inputs) != 1 || base.inputs[0] != "hello" {
		t.Fatalf("inputs = %+v", base.inputs)
	}
	if len(base.actions) != 1 {
		t.Fatalf("actions = %+v", base.actions)
	}
	base.mu.Unlock()

	base.events <- protocol.ModelRequestStarted{Reason: "user", EstimatedInputTokens: 1200}
	select {
	case ev := <-tap.Events():
		if _, ok := ev.(protocol.ModelRequestStarted); !ok {
			t.Fatalf("event = %T, want ModelRequestStarted", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tapped event")
	}
	state := hub.State()
	if len(state.Evidence) == 0 {
		t.Fatal("hub did not observe event evidence")
	}
	found := false
	for _, edge := range state.Edges {
		if edge.From == "engine" && edge.To == "model" && edge.Status == "active" {
			found = true
		}
	}
	if !found {
		t.Fatalf("state missing active engine->model edge: %+v", state.Edges)
	}
}
