package observatory

import (
	"context"
	"sync"

	"github.com/zhanglvtao/cece/internal/ipc"
	"github.com/zhanglvtao/cece/internal/protocol"
)

type EventTapRuntime struct {
	Base ipc.Runtime
	Hub  *Hub

	events    chan protocol.Event
	startOnce sync.Once
}

func NewEventTapRuntime(base ipc.Runtime, hub *Hub) *EventTapRuntime {
	r := &EventTapRuntime{Base: base, Hub: hub, events: make(chan protocol.Event, 4096)}
	r.start()
	return r
}

func (r *EventTapRuntime) Input(ctx context.Context, input string) error {
	return r.Base.Input(ctx, input)
}

func (r *EventTapRuntime) Do(action protocol.Action) {
	r.Base.Do(action)
}

func (r *EventTapRuntime) Events() <-chan protocol.Event {
	return r.events
}

func (r *EventTapRuntime) Emit(ev protocol.Event) {
	if ev == nil {
		return
	}
	r.Hub.Observe(ev)
	r.events <- ev
}

func (r *EventTapRuntime) Wait() {
	r.Base.Wait()
}

func (r *EventTapRuntime) start() {
	r.startOnce.Do(func() {
		go func() {
			defer close(r.events)
			for ev := range r.Base.Events() {
				r.Hub.Observe(ev)
				r.events <- ev
			}
			r.Hub.Close()
		}()
	})
}
