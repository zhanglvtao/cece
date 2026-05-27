package aiden

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"cece/internal/chat"
	"cece/internal/logger"
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
	Index   int           `json:"index"`
	ID      string        `json:"id"`
	Function FunctionDelta `json:"function"`
}

type FunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type parserState struct {
	messageStarted    bool
	thinkingOpen      bool
	thinkingIndex     int
	activeToolIndices map[int]bool
	textBlockStarted  bool
}

func DecodeStreamEvent(body io.ReadCloser) <-chan chat.ApiStreamEvent {
	out := make(chan chat.ApiStreamEvent)

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
				out <- chat.ApiStreamEvent{Done: true}
				return
			}

			var chunk Chunk
			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				out <- chat.ApiStreamEvent{Err: err}
				continue
			}

			emitChunk(&chunk, out, state)
		}

		if err := scanner.Err(); err != nil {
			out <- chat.ApiStreamEvent{Err: err}
		}
	}()

	return out
}

func emitChunk(chunk *Chunk, out chan<- chat.ApiStreamEvent, state *parserState) {
	if len(chunk.Choices) == 0 {
		return
	}
	choice := chunk.Choices[0]
	delta := choice.Delta

	if !state.messageStarted {
		state.messageStarted = true
		out <- chat.ApiStreamEvent{
			EventType:   "message_start",
			InputTokens: chunk.Usage.PromptTokens,
		}
	}

	if delta.ReasoningContent != "" {
		if !state.thinkingOpen {
			state.thinkingOpen = true
			state.thinkingIndex = 0
			out <- chat.ApiStreamEvent{
				EventType:  "content_block_start",
				Index:      0,
				IsThinking: true,
			}
		}
		out <- chat.ApiStreamEvent{
			EventType:     "content_block_delta",
			Detail:        "thinking_delta",
			ThinkingDelta: delta.ReasoningContent,
			Index:         state.thinkingIndex,
		}
	}

	if state.thinkingOpen && delta.Content != "" {
		out <- chat.ApiStreamEvent{
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
			out <- chat.ApiStreamEvent{
				EventType: "content_block_start",
				Index:     textIndex,
			}
		}
		out <- chat.ApiStreamEvent{
			Delta:      delta.Content,
			EventType: "content_block_delta",
			Detail:    "text_delta",
		}
	}

	for _, tc := range delta.ToolCalls {
		if state.activeToolIndices == nil {
			state.activeToolIndices = make(map[int]bool)
		}

		if tc.ID != "" && tc.Function.Name != "" {
			for idx := range state.activeToolIndices {
				if idx < tc.Index {
					out <- chat.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
					delete(state.activeToolIndices, idx)
				}
			}
			state.activeToolIndices[tc.Index] = true
			out <- chat.ApiStreamEvent{
				EventType:    "content_block_start",
				ToolCallID:   tc.ID,
				ToolCallName: tc.Function.Name,
				Index:        tc.Index,
			}
		}

		if tc.Function.Arguments != "" {
			out <- chat.ApiStreamEvent{
				EventType:     "content_block_delta",
				Detail:        "input_json_delta",
				ToolCallInput: tc.Function.Arguments,
				Index:         tc.Index,
			}
		}
	}

	if choice.FinishReason != "" {
		if state.thinkingOpen {
			out <- chat.ApiStreamEvent{
				EventType:  "content_block_stop",
				Index:      state.thinkingIndex,
				IsThinking: true,
			}
			state.thinkingOpen = false
		}

		for idx := range state.activeToolIndices {
			out <- chat.ApiStreamEvent{EventType: "content_block_stop", Index: idx}
		}

		if chunk.Usage.PromptTokens > 0 {
			out <- chat.ApiStreamEvent{
				EventType:   "message_start",
				InputTokens: chunk.Usage.PromptTokens,
			}
		}

		out <- chat.ApiStreamEvent{
			EventType:    "message_delta",
			StopReason:   mapStopReason(choice.FinishReason),
			OutputTokens: chunk.Usage.CompletionTokens,
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
