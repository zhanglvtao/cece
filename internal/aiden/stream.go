package aiden

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/logger"
)

type Chunk struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type Delta struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	ToolCalls        []ToolCallDelta `json:"tool_calls"`
}

type ToolCallDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id"`
	Function FunctionDelta `json:"function"`
}

type FunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type Usage struct {
	PromptTokens        int                 `json:"prompt_tokens"`
	CompletionTokens    int                 `json:"completion_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	PromptTokensDetails PromptTokensDetails `json:"prompt_tokens_details"`
}

type ResponsesEvent struct {
	Type        string              `json:"type"`
	Delta       string              `json:"delta"`
	Text        string              `json:"text"`
	OutputIndex int                 `json:"output_index"`
	Item        ResponsesOutputItem `json:"item"`
	Response    ResponsesPayload    `json:"response"`
}

type ResponsesOutputItem struct {
	Type              string                 `json:"type"`
	ID                string                 `json:"id"`
	CallID            string                 `json:"call_id"`
	Name              string                 `json:"name"`
	Arguments         string                 `json:"arguments"`
	Summary           []ResponsesSummaryItem `json:"summary"`
	EncryptedContent  string                 `json:"encrypted_content"`
}

type ResponsesSummaryItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ResponsesPayload struct {
	Status string         `json:"status"`
	Usage  ResponsesUsage `json:"usage"`
}

type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type InputTokenDetails struct {
	CacheRead int `json:"cache_read"`
}

type ResponsesUsage struct {
	InputTokens        int                `json:"input_tokens"`
	OutputTokens       int                `json:"output_tokens"`
	TotalTokens        int                `json:"total_tokens"`
	InputTokensDetails InputTokensDetails `json:"input_tokens_details"`
	InputTokenDetails  InputTokenDetails  `json:"input_token_details"`
}

type parserState struct {
	messageStarted       bool
	thinkingOpen         bool
	thinkingIndex        int
	reasoningOpen        bool
	reasoningIndex       int
	reasoningSeen        bool
	reasoningProviderID  string // cached from output_item.added for stop events
	reasoningSummaryText string // cached from output_item.added for stop events
	toolCallsSeen        bool
	activeToolIndices    map[int]bool
	textBlockStarted     bool
	terminalChunkSeen    bool
	doneEmitted          bool
}

func DecodeStreamEvent(body io.ReadCloser) <-chan agent.ApiStreamEvent {
	out := make(chan agent.ApiStreamEvent)

	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

		state := &parserState{}

		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("aiden sse raw line", "line", line)

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

			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(dataStr), &envelope); err == nil && strings.HasPrefix(envelope.Type, "response.") {
				var event ResponsesEvent
				if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
					out <- agent.ApiStreamEvent{Err: err}
					continue
				}
				emitResponsesEvent(&event, out, state)
				continue
			}

			var chunk Chunk
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				out <- agent.ApiStreamEvent{Err: err}
				continue
			}

			emitChunk(&chunk, out, state)
		}

		if err := scanner.Err(); err != nil {
			out <- agent.ApiStreamEvent{Err: err}
			return
		}

		// Stream ended without [DONE] — close open blocks and emit Done.
		if state.thinkingOpen {
			out <- agent.ApiStreamEvent{
				EventType:  "content_block_stop",
				Index:      state.thinkingIndex,
				IsThinking: true,
			}
		}
		if state.reasoningOpen {
			rid := state.reasoningProviderID
			rtext := state.reasoningSummaryText
			out <- agent.ApiStreamEvent{
				EventType:           "content_block_stop",
				Index:               state.reasoningIndex,
				IsThinking:          true,
				ThinkingProviderID:  rid,
				ThinkingSummaryText: rtext,
			}
		}
		for idx := range state.activeToolIndices {
			out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
		}
		if !state.doneEmitted {
			state.doneEmitted = true
			out <- agent.ApiStreamEvent{Done: true}
		}
	}()

	return out
}

func emitResponsesEvent(event *ResponsesEvent, out chan<- agent.ApiStreamEvent, state *parserState) {
	switch event.Type {
	case "response.created":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}

	case "response.in_progress":
		// No-op: acknowledge the event for logging, no state change needed.

	case "response.output_item.done":
		// output_item.done signals the completion of an output item.
		// For function_call items, this may carry the final arguments
		// if no response.function_call_arguments.delta events were sent.
		if event.Item.Type == "function_call" {
			if _, ok := state.activeToolIndices[event.OutputIndex]; !ok {
				// Function call was sent as a single item with
				// arguments (no delta streaming). Emit the full block.
				state.activeToolIndices[event.OutputIndex] = true
				out <- agent.ApiStreamEvent{
					EventType:          "content_block_start",
					ToolCallID:         event.Item.CallID,
					ToolCallProviderID: event.Item.ID,
					ToolCallName:       event.Item.Name,
					Index:              event.OutputIndex,
				}
				if event.Item.Arguments != "" {
					out <- agent.ApiStreamEvent{
						EventType:     "content_block_delta",
						Detail:        "input_json_delta",
						ToolCallInput: event.Item.Arguments,
						Index:         event.OutputIndex,
					}
				}
			}
			// Close the tool block and remove from active set so
			// response.completed doesn't double-close.
			out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: event.OutputIndex}
			delete(state.activeToolIndices, event.OutputIndex)
		}
		if event.Item.Type == "reasoning" {
			if state.reasoningOpen {
				rid := state.reasoningProviderID
				rtext := state.reasoningSummaryText
				logger.Debug("reasoning output_item.done (reasoningOpen=true)", "id", rid, "index", state.reasoningIndex)
				out <- agent.ApiStreamEvent{
					EventType:           "content_block_stop",
					Index:               state.reasoningIndex,
					IsThinking:          true,
					ThinkingProviderID:  rid,
					ThinkingSummaryText: rtext,
				}
				state.reasoningOpen = false
			} else {
				// reasoning_text.done already closed this block.
				// But output_item.done may carry encrypted_content or ID
				// that we need for round-trip serialization. Log it.
				logger.Debug("reasoning output_item.done (reasoningOpen=false, already closed)", "id", event.Item.ID, "hasEncryptedContent", event.Item.EncryptedContent != "")
			}
		}

	case "response.output_text.delta":
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		if !state.textBlockStarted {
			state.textBlockStarted = true
			out <- agent.ApiStreamEvent{EventType: "content_block_start", Index: event.OutputIndex}
		}
		if event.Delta != "" {
			out <- agent.ApiStreamEvent{
				Delta:     event.Delta,
				EventType: "content_block_delta",
				Detail:    "text_delta",
				Index:     event.OutputIndex,
			}
		}
	case "response.output_text.done":
		if state.textBlockStarted || event.Text == "" {
			return
		}
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		state.textBlockStarted = true
		out <- agent.ApiStreamEvent{EventType: "content_block_start", Index: event.OutputIndex}
		out <- agent.ApiStreamEvent{
			Delta:     event.Text,
			EventType: "content_block_delta",
			Detail:    "text_delta",
			Index:     event.OutputIndex,
		}

	case "response.output_item.added":
		switch event.Item.Type {
		case "function_call":
			if !state.messageStarted {
				state.messageStarted = true
				out <- agent.ApiStreamEvent{EventType: "message_start"}
			}
			if state.activeToolIndices == nil {
				state.activeToolIndices = make(map[int]bool)
			}
			state.activeToolIndices[event.OutputIndex] = true
			state.toolCallsSeen = true
			out <- agent.ApiStreamEvent{
				EventType:          "content_block_start",
				ToolCallID:         event.Item.CallID,
				ToolCallProviderID: event.Item.ID,
				ToolCallName:       event.Item.Name,
				Index:              event.OutputIndex,
			}
			if event.Item.Arguments != "" {
				out <- agent.ApiStreamEvent{
					EventType:     "content_block_delta",
					Detail:        "input_json_delta",
					ToolCallInput: event.Item.Arguments,
					Index:         event.OutputIndex,
				}
			}
		case "reasoning":
			if !state.messageStarted {
				state.messageStarted = true
				out <- agent.ApiStreamEvent{EventType: "message_start"}
			}
			state.reasoningOpen = true
			state.reasoningIndex = event.OutputIndex
			state.reasoningSeen = true
			state.reasoningProviderID = event.Item.ID
			summaryText := ""
			if len(event.Item.Summary) > 0 {
				summaryText = event.Item.Summary[0].Text
			}
			state.reasoningSummaryText = summaryText
			logger.Debug("reasoning output_item.added", "id", event.Item.ID, "index", event.OutputIndex, "hasEncryptedContent", event.Item.EncryptedContent != "")
			out <- agent.ApiStreamEvent{
				EventType:           "content_block_start",
				Index:               event.OutputIndex,
				IsThinking:          true,
				ThinkingProviderID:  event.Item.ID,
				ThinkingSummaryText: summaryText,
				ThinkingEncryptedContent: event.Item.EncryptedContent,
			}
			if summaryText != "" {
				out <- agent.ApiStreamEvent{
					EventType:     "content_block_delta",
					Detail:        "thinking_delta",
					ThinkingDelta: summaryText,
					Index:         event.OutputIndex,
				}
			}
		default:
			logger.Debug("aiden unhandled output_item type", "item_type", event.Item.Type)
		}

	case "response.function_call_arguments.delta":
		if event.Delta != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: event.Delta,
				Index:         event.OutputIndex,
			}
		}
	case "response.reasoning_text.delta":
		if !state.reasoningOpen {
			// Reasoning text delta received without a prior output_item.added.
			// Some proxy implementations omit the output_item.added event for
			// reasoning items. Create the reasoning block on the first delta.
			logger.Warn("reasoning_text.delta received without output_item.added — creating reasoning block on the fly", "output_index", event.OutputIndex)
			state.reasoningOpen = true
			state.reasoningIndex = event.OutputIndex
			state.reasoningSeen = true
			if !state.messageStarted {
				state.messageStarted = true
				out <- agent.ApiStreamEvent{EventType: "message_start"}
			}
			out <- agent.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     event.OutputIndex,
				IsThinking: true,
			}
		}
		if event.Delta != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "thinking_delta",
				ThinkingDelta: event.Delta,
				Index:         state.reasoningIndex,
			}
		}
	case "response.reasoning_text.done":
		if state.reasoningOpen {
			rid := state.reasoningProviderID
			rtext := state.reasoningSummaryText
			logger.Debug("reasoning_text.done (closing reasoning block)", "id", rid, "index", state.reasoningIndex)
			out <- agent.ApiStreamEvent{
				EventType:           "content_block_stop",
				Index:               state.reasoningIndex,
				IsThinking:          true,
				ThinkingProviderID:  rid,
				ThinkingSummaryText: rtext,
			}
			state.reasoningOpen = false
		}
	case "response.reasoning_summary_text.delta":
		if !state.reasoningOpen {
			logger.Warn("reasoning_summary_text.delta received without output_item.added — creating reasoning block on the fly", "output_index", event.OutputIndex)
			state.reasoningOpen = true
			state.reasoningIndex = event.OutputIndex
			state.reasoningSeen = true
			if !state.messageStarted {
				state.messageStarted = true
				out <- agent.ApiStreamEvent{EventType: "message_start"}
			}
			out <- agent.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     event.OutputIndex,
				IsThinking: true,
			}
		}
		if state.reasoningOpen && event.Delta != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "thinking_delta",
				ThinkingDelta: event.Delta,
				Index:         state.reasoningIndex,
			}
		}
	case "response.reasoning_summary_text.done":
		if state.reasoningOpen {
			rid := state.reasoningProviderID
			rtext := state.reasoningSummaryText
			logger.Debug("reasoning_summary_text.done (closing reasoning block)", "id", rid, "index", state.reasoningIndex)
			out <- agent.ApiStreamEvent{
				EventType:           "content_block_stop",
				Index:               state.reasoningIndex,
				IsThinking:          true,
				ThinkingProviderID:  rid,
				ThinkingSummaryText: rtext,
			}
			state.reasoningOpen = false
		}

	case "response.completed":
		hadTools := len(state.activeToolIndices) > 0 || state.toolCallsSeen
		for idx := range state.activeToolIndices {
			out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
		}
		state.activeToolIndices = nil
		hadReasoning := state.reasoningOpen
		if state.reasoningOpen {
			rid := state.reasoningProviderID
			rtext := state.reasoningSummaryText
			out <- agent.ApiStreamEvent{
				EventType:           "content_block_stop",
				Index:               state.reasoningIndex,
				IsThinking:          true,
				ThinkingProviderID:  rid,
				ThinkingSummaryText: rtext,
			}
			state.reasoningOpen = false
		}
		stopReason := "end_turn"
		if hadTools {
			stopReason = "tool_use"
		} else if event.Response.Status != "" {
			stopReason = mapResponsesStopReason(event.Response.Status)
		}
		if !hadTools && !state.textBlockStarted && !hadReasoning && !state.reasoningSeen {
			logger.Warn("aiden response completed with no output",
				"status", event.Response.Status,
				"input_tokens", event.Response.Usage.InputTokens,
				"output_tokens", event.Response.Usage.OutputTokens,
			)
		}
		cacheRead := event.Response.Usage.InputTokensDetails.CachedTokens
		if cacheRead == 0 {
			cacheRead = event.Response.Usage.InputTokenDetails.CacheRead
		}
		out <- agent.ApiStreamEvent{
			EventType:       "message_delta",
			StopReason:      stopReason,
			InputTokens:     event.Response.Usage.InputTokens,
			OutputTokens:    event.Response.Usage.OutputTokens,
			CacheReadTokens: cacheRead,
		}
		if !state.doneEmitted {
			state.doneEmitted = true
			out <- agent.ApiStreamEvent{Done: true}
		}

	default:
		logger.Debug("aiden unhandled response event", "type", event.Type)
	}
}

func emitChunk(chunk *Chunk, out chan<- agent.ApiStreamEvent, state *parserState) {
	// DeepSeek and some OpenAI-compatible APIs send usage in a separate
	// final chunk with empty choices. Process usage before the early return.
	if len(chunk.Choices) == 0 {
		emitUsageIfPresent(chunk, out, state)
		return
	}
	choice := chunk.Choices[0]
	delta := choice.Delta

	if !state.messageStarted {
		state.messageStarted = true
		out <- agent.ApiStreamEvent{
			EventType:   "message_start",
			InputTokens: chunk.Usage.PromptTokens,
		}
	}

	if delta.ReasoningContent != "" {
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
			ThinkingDelta: delta.ReasoningContent,
			Index:         state.thinkingIndex,
		}
	}

	if state.thinkingOpen && delta.Content != "" {
		out <- agent.ApiStreamEvent{
			EventType:  "content_block_stop",
			Index:      state.thinkingIndex,
			IsThinking: true,
		}
		state.thinkingOpen = false
	}

	if delta.Content != "" {
		if !state.textBlockStarted {
			state.textBlockStarted = true
			textIndex := 0
			if state.thinkingIndex >= 0 {
				textIndex = state.thinkingIndex + 1
			}
			out <- agent.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     textIndex,
			}
		}
		out <- agent.ApiStreamEvent{
			Delta:     delta.Content,
			EventType: "content_block_delta",
			Detail:    "text_delta",
		}
	}

	for _, tc := range delta.ToolCalls {
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
		}

		if tc.ID != "" && tc.Function.Name != "" {
			state.activeToolIndices[tc.Index] = true
			out <- agent.ApiStreamEvent{
				EventType:    "content_block_start",
				ToolCallID:   tc.ID,
				ToolCallName: tc.Function.Name,
				Index:        tc.Index,
			}
		}

		if tc.Function.Arguments != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: tc.Function.Arguments,
				Index:         tc.Index,
			}
		}
	}

	if choice.FinishReason != "" {
		state.terminalChunkSeen = true

		if state.thinkingOpen {
			out <- agent.ApiStreamEvent{
				EventType:  "content_block_stop",
				Index:      state.thinkingIndex,
				IsThinking: true,
			}
			state.thinkingOpen = false
		}

		for idx := range state.activeToolIndices {
			out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
		}

		out <- agent.ApiStreamEvent{
			EventType:       "message_delta",
			StopReason:      mapStopReason(choice.FinishReason),
			InputTokens:     chunk.Usage.PromptTokens,
			OutputTokens:    chunk.Usage.CompletionTokens,
			CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
		}
	}
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

// emitUsageIfPresent sends a message_delta with usage data from a chunk that
// has no choices but has usage info. DeepSeek and some OpenAI-compatible APIs
// send usage in a separate final chunk with empty choices, after the
// finish_reason chunk. This can overwrite a zero-usage message_delta emitted
// by the finish_reason chunk.
func emitUsageIfPresent(chunk *Chunk, out chan<- agent.ApiStreamEvent, state *parserState) {
	if chunk.Usage.PromptTokens == 0 && chunk.Usage.CompletionTokens == 0 {
		return
	}
	out <- agent.ApiStreamEvent{
		EventType:       "message_delta",
		StopReason:      "", // leave empty — usage-only chunk must not overwrite the real stop reason
		InputTokens:     chunk.Usage.PromptTokens,
		OutputTokens:    chunk.Usage.CompletionTokens,
		CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
	}
}

func mapResponsesStopReason(status string) string {
	switch status {
	case "completed", "":
		return "end_turn"
	case "incomplete":
		return "max_tokens"
	default:
		return status
	}
}
