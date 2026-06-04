// Package testkit provides shared end-to-end test infrastructure for
// driving the cece TUI ↔ engine pipeline without launching a real LLM
// or terminal. It is intentionally only useful from `_test.go` files.
package testkit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"cece/internal/agent"
	"cece/internal/tool"
)

// ScriptedTurn describes the SSE events that the fake LLM should emit
// in response to one Stream() call. Provide either Text (convenience
// for plain assistant text) or Events (full control), and optionally
// an error or a hook to inspect the request inputs.
type ScriptedTurn struct {
	// Text, if non-empty, is converted into a minimal text-only SSE
	// stream (message_start → text → message_delta end_turn → message_stop).
	Text string

	// Events overrides Text when non-empty. Each event is forwarded
	// verbatim on the channel returned by Stream().
	Events []agent.ApiStreamEvent

	// Err, if non-nil, is returned from Stream() before any events are
	// queued, simulating a transport error.
	Err error

	// BeforeFn is invoked synchronously inside Stream() before any
	// events are produced. Use it to assert on the request payload.
	BeforeFn func(messages []agent.Message, tools []tool.Definition, maxTokens int)

	// InputTokens / OutputTokens override the default usage numbers
	// when Text is used. Ignored when Events is set.
	InputTokens  int
	OutputTokens int
}

// ScriptedClient is an agent.ModelClient that replays a list of
// ScriptedTurn entries — one per Stream() invocation. Excess calls
// return an error so tests fail loudly rather than silently hanging.
type ScriptedClient struct {
	mu    sync.Mutex
	turns []ScriptedTurn
	calls atomic.Int32

	// Recorded inputs (deep-copied) for assertions in tests.
	recMu    sync.Mutex
	recorded [][]agent.Message
}

// NewScriptedClient creates a ScriptedClient with an initial script.
// Additional turns can be appended later via Append().
func NewScriptedClient(turns ...ScriptedTurn) *ScriptedClient {
	c := &ScriptedClient{}
	c.turns = append(c.turns, turns...)
	return c
}

// Append adds turns to the script. Safe to call concurrently.
func (c *ScriptedClient) Append(turns ...ScriptedTurn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = append(c.turns, turns...)
}

// Calls returns how many times Stream() was invoked.
func (c *ScriptedClient) Calls() int { return int(c.calls.Load()) }

// Recorded returns a deep copy of all messages received across Stream() calls.
func (c *ScriptedClient) Recorded() [][]agent.Message {
	c.recMu.Lock()
	defer c.recMu.Unlock()
	out := make([][]agent.Message, len(c.recorded))
	for i, msgs := range c.recorded {
		cp := make([]agent.Message, len(msgs))
		copy(cp, msgs)
		out[i] = cp
	}
	return out
}

// Stream implements agent.ModelClient.
func (c *ScriptedClient) Stream(_ context.Context, messages []agent.Message, _ agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	idx := int(c.calls.Add(1)) - 1

	// Record inputs for later assertion.
	cp := make([]agent.Message, len(messages))
	copy(cp, messages)
	c.recMu.Lock()
	c.recorded = append(c.recorded, cp)
	c.recMu.Unlock()

	c.mu.Lock()
	if idx >= len(c.turns) {
		c.mu.Unlock()
		return nil, fmt.Errorf("scripted_client: unscripted turn %d (only %d turns configured)", idx+1, len(c.turns))
	}
	turn := c.turns[idx]
	c.mu.Unlock()

	if turn.BeforeFn != nil {
		turn.BeforeFn(cp, tools, maxTokens)
	}
	if turn.Err != nil {
		return nil, turn.Err
	}

	events := turn.Events
	if len(events) == 0 {
		events = textOnlyEvents(turn.Text, turn.InputTokens, turn.OutputTokens)
	}

	out := make(chan agent.ApiStreamEvent, len(events))
	for _, ev := range events {
		out <- ev
	}
	close(out)
	return out, nil
}

// textOnlyEvents builds a minimal SSE sequence that streams a single
// text content block and ends the turn naturally.
func textOnlyEvents(text string, inputTokens, outputTokens int) []agent.ApiStreamEvent {
	if inputTokens <= 0 {
		inputTokens = 8
	}
	if outputTokens <= 0 {
		outputTokens = 4
	}
	events := []agent.ApiStreamEvent{
		{EventType: "message_start", InputTokens: inputTokens},
		{EventType: "content_block_start", Index: 0, Detail: "text"},
	}
	if text != "" {
		events = append(events, agent.ApiStreamEvent{Delta: text, Detail: "text_delta"})
	}
	events = append(events,
		agent.ApiStreamEvent{EventType: "message_delta", StopReason: "end_turn", OutputTokens: outputTokens},
		agent.ApiStreamEvent{Done: true, EventType: "message_stop"},
	)
	return events
}

// ── Fluent turn constructors ───────────────────────────────────────────────

// TextTurn produces a turn that streams a single text block.
func TextTurn(text string) ScriptedTurn {
	return ScriptedTurn{Text: text}
}

// ToolCall describes one tool_use block in MultiToolTurn.
type ToolCall struct {
	ID    string
	Name  string
	Input string // JSON-encoded input
}

// ToolUseTurn produces a turn that ends with stop_reason=tool_use and
// emits a single tool_use content block.
func ToolUseTurn(toolID, toolName, inputJSON string) ScriptedTurn {
	return MultiToolTurn(ToolCall{ID: toolID, Name: toolName, Input: inputJSON})
}

// MultiToolTurn produces a turn with N parallel tool_use blocks.
func MultiToolTurn(calls ...ToolCall) ScriptedTurn {
	events := []agent.ApiStreamEvent{
		{EventType: "message_start", InputTokens: 8},
	}
	for i, c := range calls {
		events = append(events,
			agent.ApiStreamEvent{
				EventType:    "content_block_start",
				Index:        i,
				Detail:       "tool_use",
				ToolCallID:   c.ID,
				ToolCallName: c.Name,
			},
			agent.ApiStreamEvent{
				Detail:        "input_json_delta",
				Index:         i,
				ToolCallID:    c.ID,
				ToolCallInput: c.Input,
			},
			agent.ApiStreamEvent{
				EventType:  "content_block_stop",
				Index:      i,
				ToolCallID: c.ID,
			},
		)
	}
	events = append(events,
		agent.ApiStreamEvent{EventType: "message_delta", StopReason: "tool_use", OutputTokens: 4},
		agent.ApiStreamEvent{Done: true, EventType: "message_stop"},
	)
	return ScriptedTurn{Events: events}
}

// ThinkingThenText produces a thinking block followed by a text block.
func ThinkingThenText(thinkingText, signature, text string) ScriptedTurn {
	events := []agent.ApiStreamEvent{
		{EventType: "message_start", InputTokens: 8},
		{EventType: "content_block_start", Index: 0, Detail: "thinking", IsThinking: true},
		{Detail: "thinking_delta", Index: 0, ThinkingDelta: thinkingText},
		{EventType: "content_block_stop", Index: 0, ThinkingSignature: signature},
		{EventType: "content_block_start", Index: 1, Detail: "text"},
		{Delta: text, Detail: "text_delta"},
		{EventType: "message_delta", StopReason: "end_turn", OutputTokens: 4},
		{Done: true, EventType: "message_stop"},
	}
	return ScriptedTurn{Events: events}
}

// ErrorTurn produces a turn that returns err from Stream() before any events.
func ErrorTurn(err error) ScriptedTurn {
	return ScriptedTurn{Err: err}
}

// StreamErrorMidTurn produces a turn that streams partialText then
// emits a stream-level error mid-flight.
func StreamErrorMidTurn(partialText string, err error) ScriptedTurn {
	events := []agent.ApiStreamEvent{
		{EventType: "message_start", InputTokens: 8},
		{EventType: "content_block_start", Index: 0, Detail: "text"},
	}
	if partialText != "" {
		events = append(events, agent.ApiStreamEvent{Delta: partialText, Detail: "text_delta"})
	}
	events = append(events, agent.ApiStreamEvent{Err: err})
	return ScriptedTurn{Events: events}
}

// MaxTokensTurn produces a turn whose stop_reason is "max_tokens" so
// the engine triggers a TruncationRetry.
func MaxTokensTurn(partialText string, outputTokens int) ScriptedTurn {
	if outputTokens <= 0 {
		outputTokens = 4
	}
	events := []agent.ApiStreamEvent{
		{EventType: "message_start", InputTokens: 8},
		{EventType: "content_block_start", Index: 0, Detail: "text"},
	}
	if partialText != "" {
		events = append(events, agent.ApiStreamEvent{Delta: partialText, Detail: "text_delta"})
	}
	events = append(events,
		agent.ApiStreamEvent{EventType: "message_delta", StopReason: "max_tokens", OutputTokens: outputTokens},
		agent.ApiStreamEvent{Done: true, EventType: "message_stop"},
	)
	return ScriptedTurn{Events: events}
}
