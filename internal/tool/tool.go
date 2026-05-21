package tool

import (
	"context"
	"encoding/json"
)

// Definition is the JSON Schema definition sent to the Anthropic API.
type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// Result is the return value of a tool execution.
type Result struct {
	Content string
	IsError bool
}

// Emitter streams tool output incrementally. May be nil.
// Tools call Emit for each output line; the Runtime converts
// each call into a ToolExecDelta event for the TUI.
type Emitter interface {
	Emit(text string)
}

// Tool is the core interface for all tools.
type Tool interface {
	Info() Definition
	Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result
}
