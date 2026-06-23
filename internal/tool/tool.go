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
	Content       string
	IsError       bool
	Truncated     bool
	OutputPath    string
	OriginalBytes int
	PreviewBytes  int
}

type ResultStoragePolicy struct {
	MaxBytes     int
	PreviewBytes int
}

type LargeResultPolicyProvider interface {
	ResultStoragePolicy() ResultStoragePolicy
}

// Emitter streams tool output incrementally. May be nil.
// Tools call Emit for each output line; the Runtime converts
// each call into a ToolExecDelta event for the TUI.
type Emitter interface {
	Emit(text string)
}

type Effect string

const (
	EffectRead  Effect = "read"
	EffectWrite Effect = "write"
	EffectExec  Effect = "exec"
	EffectMode  Effect = "mode"
)

// Effectful is an optional interface for tools to declare their side-effect class.
type Effectful interface {
	Effect() Effect
}

func EffectOf(t Tool) Effect {
	if e, ok := t.(Effectful); ok {
		return e.Effect()
	}
	return EffectExec
}

// Tool is the core interface for all tools.
type Tool interface {
	Info() Definition
	Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result
}
