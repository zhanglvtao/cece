package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// Registry manages registered tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a Registry with the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Info().Name] = t
	}
	return r
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Definitions returns all tool definitions for the API request.
func (r *Registry) Definitions() []Definition {
	defs := make([]Definition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Info())
	}
	return defs
}

// Execute runs a tool by name with the given input and emitter.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage, emitter Emitter) Result {
	t, ok := r.tools[name]
	if !ok {
		return Result{Content: fmt.Sprintf("unknown tool: %s", name), IsError: true}
	}
	return t.Run(ctx, input, emitter)
}
