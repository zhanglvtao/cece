package aiden

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/logger"
)

// Responses API SSE event types (OpenAI Responses API streaming protocol).
// These differ from Chat Completions SSE events.

// responsesSSEEvent is the raw SSE envelope for Responses API streaming.
// The "type" field determines the event kind; other fields vary by type.
type responsesSSEEvent struct {
	Type string `json:"type"`

	// response.created / response.completed
	Response *responsesResponse `json:"response,omitempty"`

	// response.output_item.added
	OutputIndex int                 `json:"output_index,omitempty"`
	Item        *responsesOutputItem `json:"item,omitempty"`

	// response.content_part.added
	ContentIndex int               `json:"content_index,omitempty"`
	Part         *responsesContentPart `json:"part,omitempty"`

	// response.output_text.delta / response.reasoning_summary_text.delta / response.function_call_arguments.delta
	Delta string `json:"delta,omitempty"`

	// error
	Error *responsesError `json:"error,omitempty"`
}

type responsesResponse struct {
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Output json.RawMessage `json:"output,omitempty"`
	Usage  *responsesUsage `json:"usage,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type responsesOutputItem struct {
	Type string `json:"type"` // "message", "function_call", "reasoning"
	ID   string `json:"id"`
	Role string `json:"role,omitempty"`
	// function_call fields
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responsesContentPart struct {
	Type string `json:"type"` // "output_text", "reasoning_summary_text"
	Text string `json:"text,omitempty"`
}

type responsesError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// responsesStreamState tracks parsing state across SSE events.
type responsesStreamState struct {
	messageStarted    bool
	thinkingOpen      bool
	thinkingIndex     int
	textBlockStarted  bool
	activeToolIndices map[int]*toolCallTracker
	doneEmitted       bool
	// Track output items for function_call detection
	pendingFuncCalls map[int]*toolCallTracker // output_index → tracker
	// sawFuncCall tracks whether any function_call output_item was seen,
	// used to determine stop_reason in response.completed.
	sawFuncCall bool
}

type toolCallTracker struct {
	id   string
	name string
}

// DecodeResponsesStream parses Responses API SSE events into the unified
// agent.ApiStreamEvent format, so the upper layer (ModelStreamer) doesn't
// need to know which API protocol was used.
func DecodeResponsesStream(body io.ReadCloser) <-chan agent.ApiStreamEvent {
	out := make(chan agent.ApiStreamEvent)

	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		state := &responsesStreamState{}

		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("aiden responses sse raw line", "line", line)

			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "[DONE]" {
				if !state.doneEmitted {
					state.doneEmitted = true
					out <- agent.ApiStreamEvent{Done: true}
				}
				return
			}

			var evt responsesSSEEvent
			if err := json.Unmarshal([]byte(dataStr), &evt); err != nil {
				out <- agent.ApiStreamEvent{Err: err}
				continue
			}

			emitResponsesEvent(&evt, out, state)
		}

		if err := scanner.Err(); err != nil {
			out <- agent.ApiStreamEvent{Err: err}
			return
		}

		// Stream ended without [DONE] — close open blocks and emit Done.
		closeOpenBlocks(out, state)
		if !state.doneEmitted {
			state.doneEmitted = true
			out <- agent.ApiStreamEvent{Done: true}
		}
	}()

	return out
}

func emitResponsesEvent(evt *responsesSSEEvent, out chan<- agent.ApiStreamEvent, state *responsesStreamState) {
	switch evt.Type {
	case "response.created":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{
				EventType: "message_start",
			}
		}

	case "response.output_item.added":
		if evt.Item != nil && evt.Item.Type == "function_call" {
			state.sawFuncCall = true
			if state.pendingFuncCalls == nil {
				state.pendingFuncCalls = make(map[int]*toolCallTracker)
			}
			state.pendingFuncCalls[evt.OutputIndex] = &toolCallTracker{
				id:   evt.Item.CallID,
				name: evt.Item.Name,
			}
			// Emit content_block_start for the tool call
			if state.activeToolIndices == nil {
				state.activeToolIndices = make(map[int]*toolCallTracker)
			}
			state.activeToolIndices[evt.OutputIndex] = &toolCallTracker{
				id:   evt.Item.CallID,
				name: evt.Item.Name,
			}
			out <- agent.ApiStreamEvent{
				EventType:    "content_block_start",
				ToolCallID:   evt.Item.CallID,
				ToolCallName: evt.Item.Name,
				Index:        evt.OutputIndex,
			}
		}

	case "response.content_part.added":
		if evt.Part != nil {
			switch evt.Part.Type {
			case "output_text":
				if !state.textBlockStarted {
					state.textBlockStarted = true
					out <- agent.ApiStreamEvent{
						EventType: "content_block_start",
						Index:     evt.OutputIndex,
					}
				}
			case "reasoning_summary_text":
				if !state.thinkingOpen {
					state.thinkingOpen = true
					state.thinkingIndex = evt.OutputIndex
					out <- agent.ApiStreamEvent{
						EventType:  "content_block_start",
						Index:      evt.OutputIndex,
						IsThinking: true,
					}
				}
			}
		}

	case "response.output_text.delta":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		// Close thinking block if transitioning to text
		if state.thinkingOpen {
			out <- agent.ApiStreamEvent{
				EventType:  "content_block_stop",
				Index:      state.thinkingIndex,
				IsThinking: true,
			}
			state.thinkingOpen = false
		}
		if !state.textBlockStarted {
			state.textBlockStarted = true
			out <- agent.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     evt.OutputIndex,
			}
		}
		out <- agent.ApiStreamEvent{
			EventType: "content_block_delta",
			Detail:    "text_delta",
			Delta:     evt.Delta,
		}

	case "response.reasoning_summary_text.delta":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		if !state.thinkingOpen {
			state.thinkingOpen = true
			state.thinkingIndex = evt.OutputIndex
			out <- agent.ApiStreamEvent{
				EventType:  "content_block_start",
				Index:      evt.OutputIndex,
				IsThinking: true,
			}
		}
		out <- agent.ApiStreamEvent{
			EventType:     "content_block_delta",
			Detail:        "thinking_delta",
			ThinkingDelta: evt.Delta,
			Index:         evt.OutputIndex,
		}

	case "response.function_call_arguments.delta":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		out <- agent.ApiStreamEvent{
			EventType:     "content_block_delta",
			Detail:        "input_json_delta",
			ToolCallInput: evt.Delta,
			Index:         evt.OutputIndex,
		}

	case "response.function_call_arguments.done":
		// Arguments complete — emit content_block_stop
		out <- agent.ApiStreamEvent{
			EventType: "content_block_stop",
			Index:     evt.OutputIndex,
		}
		if state.activeToolIndices != nil {
			delete(state.activeToolIndices, evt.OutputIndex)
		}

	case "response.completed":
		closeOpenBlocks(out, state)

		var inputTokens, outputTokens int
		if evt.Response != nil && evt.Response.Usage != nil {
			inputTokens = evt.Response.Usage.InputTokens
			outputTokens = evt.Response.Usage.OutputTokens
		}

		// Responses API doesn't have an explicit stop_reason field.
		// Determine it from whether function_call items were in the output:
		// - function_call present → "tool_use" (model wants to call tools)
		// - no function_call → "end_turn" (model finished its response)
		stopReason := "end_turn"
		if state.sawFuncCall {
			stopReason = "tool_use"
		}

		out <- agent.ApiStreamEvent{
			EventType:    "message_delta",
			StopReason:   stopReason,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		}

		if !state.doneEmitted {
			state.doneEmitted = true
			out <- agent.ApiStreamEvent{Done: true}
		}

	case "error":
		errMsg := "unknown responses api error"
		if evt.Error != nil {
			if evt.Error.Message != "" {
				errMsg = evt.Error.Message
			} else if evt.Error.Code != "" {
				errMsg = evt.Error.Code
			}
		}
		out <- agent.ApiStreamEvent{Err: fmt.Errorf("responses api error: %s", errMsg)}
	}
}

func closeOpenBlocks(out chan<- agent.ApiStreamEvent, state *responsesStreamState) {
	if state.thinkingOpen {
		out <- agent.ApiStreamEvent{
			EventType:  "content_block_stop",
			Index:      state.thinkingIndex,
			IsThinking: true,
		}
		state.thinkingOpen = false
	}
	if state.textBlockStarted {
		state.textBlockStarted = false
	}
	for idx := range state.activeToolIndices {
		out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
	}
	if state.activeToolIndices != nil {
		state.activeToolIndices = nil
	}
}
