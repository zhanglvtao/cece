package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/zhanglvtao/cece/internal/engine"
	"github.com/zhanglvtao/cece/internal/protocol"
	"github.com/zhanglvtao/cece/internal/session"
	"github.com/zhanglvtao/cece/internal/skill"
)

// RuntimeHost is the process-level runtime root exposed to ipc/stdin clients.
// In the first iteration it is a thin host over the foreground interactive runtime.
type RuntimeHost struct {
	foreground *Bundle
	closeOnce  sync.Once
}

func BuildHost(opts Options) (*RuntimeHost, error) {
	bundle, err := Build(opts)
	if err != nil {
		return nil, err
	}
	return NewHost(bundle), nil
}

func NewHost(bundle *Bundle) *RuntimeHost {
	host := &RuntimeHost{foreground: bundle}
	model := ""
	if bundle != nil && bundle.Engine != nil {
		model = bundle.Engine.SessionMetaModel()
	}
	slog.Info("runtime host: started", "model", model)
	return host
}

func (h *RuntimeHost) Input(ctx context.Context, input string) error {
	slog.Info("runtime host: input", "operation", "input")
	return h.foreground.Engine.Input(ctx, input)
}

func (h *RuntimeHost) Do(action protocol.Action) {
	slog.Info("runtime host: action", "operation", "do", "action", fmt.Sprintf("%T", action))
	h.foreground.Mediator.Do(action)
}

func (h *RuntimeHost) Events() <-chan protocol.Event {
	return h.foreground.Engine.Events()
}

func (h *RuntimeHost) Wait() {
	h.foreground.Mediator.Wait()
}

func (h *RuntimeHost) Close() {
	h.closeOnce.Do(func() {
		slog.Info("runtime host: shutdown")
		if h.foreground != nil && h.foreground.Cleanup != nil {
			h.foreground.Cleanup()
		}
	})
}

func (h *RuntimeHost) Engine() *engine.Engine {
	if h == nil || h.foreground == nil {
		return nil
	}
	return h.foreground.Engine
}

func (h *RuntimeHost) Mediator() *engine.EngineMediator {
	if h == nil || h.foreground == nil {
		return nil
	}
	return h.foreground.Mediator
}

func (h *RuntimeHost) Store() session.Store {
	if h == nil || h.foreground == nil {
		return nil
	}
	return h.foreground.Store
}

func (h *RuntimeHost) Skills() *skill.Store {
	if h == nil || h.foreground == nil {
		return nil
	}
	return h.foreground.Skills
}
