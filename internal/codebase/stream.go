package codebase

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"cece/internal/agent"
	"cece/internal/logger"
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
	textBlockStarted  bool
	textBlockIndex    int
	inputTokens       int
	outputTokens      int
}

// DecodeStreamEvent reads a codebase-api SSE stream and emits agent.ApiStreamEvent values.
func DecodeStreamEvent(body io.ReadCloser) <-chan agent.ApiStreamEvent {
	out := make(chan agent.ApiStreamEvent)

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

			if !strings.HasPrefix(line, "data:") {
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
		}
	}()

	return out
}

func processEvent(eventType, data string, out chan<- agent.ApiStreamEvent, state *streamState) {
	switch eventType {
	case "output":
		var ev OutputEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
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
			Delta:      ev.Response,
			EventType:  "content_block_delta",
			Detail:     "text_delta",
			Index:      state.textBlockIndex,
		}
	}

	// Tool calls
	for _, tc := range ev.ToolCalls {
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
		}

		// First appearance: tool_use start (has id + name)
		if tc.ID != "" {
			// Skip tool calls with empty name or missing function info.
			if tc.FunctionCall == nil || tc.FunctionCall.Name == "" {
				logger.Debug("codebase skipping tool call with empty name", "index", tc.Index, "id", tc.ID)
			} else {
				// Close any previously opened tool calls with smaller indices
				for idx := range state.activeToolIndices {
					if idx < tc.Index {
						out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
						delete(state.activeToolIndices, idx)
					}
				}
				state.activeToolIndices[tc.Index] = true
				out <- agent.ApiStreamEvent{
					EventType:    "content_block_start",
					ToolCallID:   tc.ID,
					ToolCallName: tc.FunctionCall.Name,
					Index:        tc.Index,
				}
			}
		}

		// Subsequent: input_json_delta (always process if arguments present)
		if tc.FunctionCall != nil && tc.FunctionCall.Arguments != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: tc.FunctionCall.Arguments,
				Index:         tc.Index,
			}
		}
	}
}

func emitDone(ev *DoneEvent, out chan<- agent.ApiStreamEvent, state *streamState) {
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
