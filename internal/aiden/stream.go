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
	OutputIndex int                 `json:"output_index"`
	Item        ResponsesOutputItem `json:"item"`
	Response    ResponsesPayload    `json:"response"`
}

type ResponsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	messageStarted    bool
	thinkingOpen      bool
	thinkingIndex     int
	activeToolIndices map[int]bool
	textBlockStarted  bool
	terminalChunkSeen bool
	doneEmitted       bool
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
	case "response.output_item.added":
		if event.Item.Type != "function_call" {
			return
		}
		if !state.messageStarted {
			state.messageStarted = true
			out <- agent.ApiStreamEvent{EventType: "message_start"}
		}
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
		}
		state.activeToolIndices[event.OutputIndex] = true
		out <- agent.ApiStreamEvent{
			EventType:    "content_block_start",
			ToolCallID:   event.Item.CallID,
			ToolCallName: event.Item.Name,
			Index:        event.OutputIndex,
		}
		if event.Item.Arguments != "" {
			out <- agent.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: event.Item.Arguments,
				Index:         event.OutputIndex,
			}
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
	case "response.completed":
		hadTools := len(state.activeToolIndices) > 0
		for idx := range state.activeToolIndices {
			out <- agent.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
		}
		state.activeToolIndices = nil
		stopReason := "end_turn"
		if event.Response.Status != "" {
			stopReason = mapResponsesStopReason(event.Response.Status)
		}
		if hadTools {
			stopReason = "tool_use"
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
	}
}

func emitChunk(chunk *Chunk, out chan<- agent.ApiStreamEvent, state *parserState) {
	if len(chunk.Choices) == 0 {
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
