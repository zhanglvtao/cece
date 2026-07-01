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
		var lineCount int

		for scanner.Scan() {
			line := scanner.Text()
			lineCount++
			logger.Debug("aiden sse raw line", "line", line)

			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataStr == "[DONE]" {
				// Close any open blocks before signaling Done.
				// Some OpenAI-compatible proxies send finish_reason="" or omit
				// it entirely, so blocks may still be open at [DONE].
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
				if !state.terminalChunkSeen && state.messageStarted {
					state.terminalChunkSeen = true
					stopReason := "end_turn"
					if len(state.activeToolIndices) > 0 {
						stopReason = "tool_use"
					}
					out <- agent.ApiStreamEvent{
						EventType:  "message_delta",
						StopReason: stopReason,
					}
				}
				if !state.doneEmitted {
					state.doneEmitted = true
					out <- agent.ApiStreamEvent{Done: true}
				}
				return
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
		logger.Warn("aiden stream ended without [DONE]", "total_lines", lineCount, "message_started", state.messageStarted, "terminal_chunk_seen", state.terminalChunkSeen)
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

		if tc.ID != "" && tc.Function.Name != "" && !state.activeToolIndices[tc.Index] {
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
		stopReason := mapStopReason(choice.FinishReason)
		if choice.FinishReason == "stop" && len(state.activeToolIndices) > 0 {
			stopReason = "tool_use"
		}

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
			StopReason:      stopReason,
			InputTokens:     chunk.Usage.PromptTokens,
			OutputTokens:    chunk.Usage.CompletionTokens,
			CacheReadTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
		}
	}
}

func mapStopReason(reason string) string {
	switch reason {
	case "stop", "": // OpenAI-compatible proxies may send empty finish_reason
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
