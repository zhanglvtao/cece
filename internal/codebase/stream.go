package codebase

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/logger"
)

// OutputEvent represents a single output event from the codebase-api SSE stream.
type OutputEvent struct {
	Response         string             `json:"response"`
	ReasoningContent string             `json:"reasoning_content"`
	ToolCalls        []CodebaseToolCall `json:"tool_calls"`
}

// TokenUsageEvent represents a token_usage event from the codebase-api SSE stream.
type TokenUsageEvent struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// DoneEvent represents a done event from the codebase-api SSE stream.
type DoneEvent struct {
	FinishReason string `json:"finish_reason"`
}

// ErrorEvent represents an error event from the codebase-api SSE stream.
type ErrorEvent struct {
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
	Code    int    `json:"code,omitempty"`
}

type streamState struct {
	messageStarted    bool
	thinkingOpen      bool
	thinkingIndex     int
	activeToolIndices map[int]bool
	toolCalls         map[int]*accumulatedToolCall
	textBlockStarted  bool
	textBlockIndex    int
	inputTokens       int
	outputTokens      int
	doneEmitted       bool
}

type accumulatedToolCall struct {
	id         string
	name       string
	startFired bool
}

// DecodeStreamEvent reads a codebase-api SSE stream and emits agent.ApiStreamEvent values.
func DecodeStreamEvent(body io.ReadCloser) <-chan agent.ApiStreamEvent {
	out := make(chan agent.ApiStreamEvent, 64)

	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		state := &streamState{}
		var currentEvent string

		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("codebase sse raw line", "line", line)

			if line == "" {
				currentEvent = ""
				continue
			}

			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}

			// Handle potentially malformed proxy output with XML-like tags, like:
			// _delta detail=text_delta delta="xxx"
			// <id>...</id>
			if strings.HasPrefix(line, "<id>") || strings.HasPrefix(line, "_delta") {
				continue
			}

			if !strings.HasPrefix(line, "data:") {
				// We still need to handle empty events if the line starts with '{' (OpenAI format fallback)
				if strings.HasPrefix(line, "{") {
					processEvent("", line, out, state)
				}
				continue
			}

			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "" {
				continue
			}

			processEvent(currentEvent, dataStr, out, state)
		}

		if err := scanner.Err(); err != nil {
			out <- agent.ApiStreamEvent{Err: err}
		} else {
			// Stream ended without done event — close open blocks and
			// emit Done so the consumer never waits forever.
			emitDone(&DoneEvent{FinishReason: "stop"}, out, state)
		}
	}()

	return out
}

func processEvent(eventType, data string, out chan<- agent.ApiStreamEvent, state *streamState) {
	switch eventType {
	case "", "output":
		var ev OutputEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			if eventType == "" {
				// If we don't have an explicit event type and it fails to parse as OutputEvent,
				// it might be a malformed line or another format. Log and ignore.
				logger.Debug("codebase empty event type unmarshal error", "error", err, "data", data)
				return
			}
			out <- agent.ApiStreamEvent{Err: err}
			return
		}
		emitOutput(&ev, out, state)

	case "token_usage":
		var ev TokenUsageEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			logger.Debug("codebase token_usage parse error", "error", err)
			return
		}
		state.inputTokens = ev.PromptTokens
		state.outputTokens = ev.CompletionTokens

	case "done":
		var ev DoneEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			out <- agent.ApiStreamEvent{Err: err}
			return
		}
		emitDone(&ev, out, state)

	case "error":
		var ev ErrorEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			out <- agent.ApiStreamEvent{Err: fmt.Errorf("codebase error event parse failure: %s", data)}
			return
		}
		message := ev.Message
		if message == "" {
			message = ev.Error
		}
		if ev.Error != "" && ev.Error != message {
			message = message + "; " + ev.Error
		}
		out <- agent.ApiStreamEvent{Err: fmt.Errorf("codebase api error: %s (code=%d)", message, ev.Code)}

	case "metadata", "progress_notice", "timing_cost", "extra_info":
		// Informational events, safely ignored

	default:
		logger.Debug("codebase unknown event type", "type", eventType, "data", data)
	}
}

func emitOutput(ev *OutputEvent, out chan<- agent.ApiStreamEvent, state *streamState) {
	// Synthesize message_start on first output
	if !state.messageStarted {
		state.messageStarted = true
		out <- agent.ApiStreamEvent{
			EventType:   "message_start",
			InputTokens: state.inputTokens,
		}
	}

	// Reasoning/thinking content
	if ev.ReasoningContent != "" {
		if !state.thinkingOpen {
			state.thinkingOpen = true
			state.thinkingIndex = 0
			out <- agent.ApiStreamEvent{
				EventType:  "content_block_start",
				Index:      0,
				IsThinking: true,
			}
		}
		out <- agent.ApiStreamEvent{
			EventType:     "content_block_delta",
			Detail:        "thinking_delta",
			ThinkingDelta: ev.ReasoningContent,
			Index:         state.thinkingIndex,
		}
	}

	// Transition from thinking to text: close thinking block
	if state.thinkingOpen && ev.Response != "" {
		out <- agent.ApiStreamEvent{
			EventType:  "content_block_stop",
			Index:      state.thinkingIndex,
			IsThinking: true,
		}
		state.thinkingOpen = false
	}

	// Regular text content
	if ev.Response != "" {
		if !state.textBlockStarted {
			state.textBlockStarted = true
			state.textBlockIndex = 0
			if state.thinkingIndex >= 0 && state.thinkingOpen == false && state.messageStarted {
				state.textBlockIndex = state.thinkingIndex + 1
			}
			out <- agent.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     state.textBlockIndex,
			}
		}
		out <- agent.ApiStreamEvent{
			Delta:     ev.Response,
			EventType: "content_block_delta",
			Detail:    "text_delta",
			Index:     state.textBlockIndex,
		}
	}

	// Tool calls
	for _, tc := range ev.ToolCalls {
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
			state.toolCalls = make(map[int]*accumulatedToolCall)
		}
		
		acc, ok := state.toolCalls[tc.Index]
		if !ok {
			acc = &accumulatedToolCall{}
			state.toolCalls[tc.Index] = acc
		}

		if tc.ID != "" {
			acc.id = tc.ID
		}

		fn := tc.effectiveFunctionCall()
		if fn != nil && fn.Name != "" {
			acc.name = fn.Name
		}

		// First appearance: tool_use start (has id + name)
		if !acc.startFired && acc.id != "" && acc.name != "" {
			state.activeToolIndices[tc.Index] = true
			acc.startFired = true
			out <- agent.ApiStreamEvent{
				EventType:    "content_block_start",
				ToolCallID:   acc.id,
				ToolCallName: acc.name,
				Index:        tc.Index,
			}
		}

		// Subsequent: input_json_delta (always process if arguments present)
		if fn != nil && fn.Arguments != "" {
			// Don't emit arguments if we haven't fired the start event yet.
			// This shouldn't happen with well-behaved APIs, but just in case.
			if acc.startFired {
				out <- agent.ApiStreamEvent{
					EventType:     "content_block_delta",
					Detail:        "input_json_delta",
					ToolCallInput: fn.Arguments,
					Index:         tc.Index,
				}
			} else {
				logger.Debug("codebase skipping tool call arguments before start event", "index", tc.Index)
			}
		}
	}
}

func emitDone(ev *DoneEvent, out chan<- agent.ApiStreamEvent, state *streamState) {
	if state.doneEmitted {
		return
	}
	state.doneEmitted = true
	// Close thinking block if still open
	if state.thinkingOpen {
		out <- agent.ApiStreamEvent{
			EventType:  "content_block_stop",
			Index:      state.thinkingIndex,
			IsThinking: true,
		}
		state.thinkingOpen = false
	}

	// Close text block if open
	if state.textBlockStarted {
		out <- agent.ApiStreamEvent{
			EventType: "content_block_stop",
			Index:     state.textBlockIndex,
		}
	}

	// Close all active tool call blocks
	for idx := range state.activeToolIndices {
		out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
	}

	out <- agent.ApiStreamEvent{
		EventType:    "message_delta",
		StopReason:   mapStopReason(ev.FinishReason),
		OutputTokens: state.outputTokens,
	}
	out <- agent.ApiStreamEvent{Done: true}
}

func mapStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

// isCodebaseRetryable checks whether a codebase API stream error
// is retryable (e.g. backend model temporarily unavailable).
func isCodebaseRetryable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "code=3003")
}
