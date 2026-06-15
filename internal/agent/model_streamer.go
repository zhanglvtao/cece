package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/tool"
)

// modelResponse holds the result of a single model stream invocation.
type modelResponse struct {
	stopReason          string
	inputTokens         int
	outputTokens        int
	toolCalls           []ApiToolUseBlock // non-empty when stopReason == "tool_use"
	textContent         string            // assistant text reply
	thinkingBlocks      []ApiContentBlock // thinking + redacted_thinking blocks
	cacheReadTokens     int               // cache read tokens from this stream
	cacheCreationTokens int               // cache creation tokens from this stream
}

// toolCallState tracks incremental assembly of a tool_use block across SSE events.
type toolCallState struct {
	id         string
	providerID string
	name       string
	input      strings.Builder
}

// ModelStreamRequest describes one streaming model call within an agent turn.
type ModelStreamRequest struct {
	Messages    []Message
	System      SystemPrompt
	Reason      string
	MaxTokens   int
	ToolResults []string
}

// ModelStreamer converts provider stream chunks into chat events and a modelResponse.
type ModelStreamer struct {
	client           ModelClient
	registry         *tool.Registry
	onInputTokens    func(int)
	lastInputTokens  int // actual input tokens from last API response
	lastMessageCount int // number of messages in last request
}

func NewModelStreamer(client ModelClient, registry *tool.Registry, onInputTokens func(int)) *ModelStreamer {
	return &ModelStreamer{client: client, registry: registry, onInputTokens: onInputTokens}
}

// Stream executes one streaming model call, emits UI events to ch,
// and returns the parsed response for the agent loop.
func (s *ModelStreamer) Stream(ctx context.Context, req ModelStreamRequest, ch chan<- Event) (modelResponse, error) {
	var tools []tool.Definition
	if s.registry != nil {
		tools = s.registry.Definitions()
	}

	estimated := EstimateRequestTokens(req.System, req.Messages, tools)

	// Calibrate using last actual InputTokens as a water level:
	// estimated = max(pure_estimate, lastActual + incremental_delta)
	if s.lastInputTokens > 0 && len(req.Messages) > s.lastMessageCount {
		delta := EstimateRequestTokens(SystemPrompt{}, req.Messages[s.lastMessageCount:], nil)
		if waterLevel := s.lastInputTokens + delta; waterLevel > estimated {
			estimated = waterLevel
		}
	}

	emitModelEvent(ch, ModelRequestStarted{
		Reason:               req.Reason,
		ToolResults:          req.ToolResults,
		EstimatedInputTokens: estimated,
	})

	chunks, err := s.client.Stream(ctx, req.Messages, req.System, tools, req.MaxTokens)
	if err != nil {
		return modelResponse{}, err
	}

	start := time.Now()

	var resp modelResponse
	var textBuf strings.Builder
	var thinkingBuf strings.Builder
	var thinkingIndex int = -1         // index of the current thinking block, -1 = none
	var thinkingProviderID string       // provider ID from content_block_start (e.g. rs_...)
	var thinkingSummaryText string      // initial summary text from content_block_start
	var redactedThinkingIndex int = -1 // index of the current redacted_thinking block, -1 = none
	var toolInputStates map[int]*toolCallState
	assistantStarted := false

	for chunk := range chunks {
		if chunk.Err != nil {
			// Provider API parameter/validation errors (code=4001, InvalidParameter,
			// required field, etc.) are non-fatal: return them as text so the
			// agent loop survives instead of crashing with RunFailed.
			if isRecoverableProviderError(chunk.Err) {
				logger.Warn("provider param error — recovering as text response", "error", chunk.Err.Error())
				text := fmt.Sprintf("[Provider Error] %s\nThe previous tool call had parameter issues that the provider rejected. You may retry.", chunk.Err.Error())
				if isContextTooLongError(chunk.Err.Error()) {
					text = fmt.Sprintf("[Context Window Exceeded] %s\nYour conversation has grown beyond the model's context window. Call the Compact tool immediately to compress history before continuing. This is your responsibility — the system will not auto-compact.", chunk.Err.Error())
				}
				emitModelEvent(ch, RunFailed{Err: chunk.Err})
				return modelResponse{
					stopReason:  "end_turn",
					textContent: text,
				}, nil
			}
			return modelResponse{}, chunk.Err
		}

		if chunk.EventType != "" && !chunk.Done {
			logger.Debug("sse event", "type", chunk.EventType, "detail", chunk.Detail, "delta", truncate(chunk.Delta, 60))
			emitModelEvent(ch, StreamEventDetail{
				EventType: chunk.EventType,
				Detail:    chunk.Detail,
				Text:      truncate(chunk.Delta, 60),
			})
		}

		if chunk.EventType == "message_start" {
			resp.inputTokens = chunk.InputTokens
			if s.onInputTokens != nil {
				s.onInputTokens(resp.inputTokens)
			}
			s.lastInputTokens = resp.inputTokens
			s.lastMessageCount = len(req.Messages)
			resp.cacheReadTokens = chunk.CacheReadTokens
			resp.cacheCreationTokens = chunk.CacheCreationTokens
			var toolNames []string
			for _, def := range tools {
				toolNames = append(toolNames, def.Name)
			}
			emitModelEvent(ch, StreamStarted{
				InputTokens:         resp.inputTokens,
				Tools:               toolNames,
				CacheCreationTokens: chunk.CacheCreationTokens,
				CacheReadTokens:     chunk.CacheReadTokens,
			})
		}
		if chunk.EventType == "message_delta" {
			resp.outputTokens = chunk.OutputTokens
			if chunk.StopReason != "" {
				resp.stopReason = chunk.StopReason
			}
			// Some providers (OpenAI-compatible) deliver final usage in message_delta
			if chunk.InputTokens > 0 {
				resp.inputTokens = chunk.InputTokens
				if s.onInputTokens != nil {
					s.onInputTokens(resp.inputTokens)
				}
				s.lastInputTokens = resp.inputTokens
			}
			if chunk.CacheReadTokens > 0 {
				resp.cacheReadTokens = chunk.CacheReadTokens
			}
			emitModelEvent(ch, StreamStarted{
				InputTokens:         resp.inputTokens,
				CacheCreationTokens: resp.cacheCreationTokens,
				CacheReadTokens:     resp.cacheReadTokens,
			})
		}

		// Tool use assembly
		if chunk.EventType == "content_block_start" && chunk.ToolCallID != "" {
			if toolInputStates == nil {
				toolInputStates = make(map[int]*toolCallState)
			}
			toolInputStates[chunk.Index] = &toolCallState{
				id:         chunk.ToolCallID,
				providerID: chunk.ToolCallProviderID,
				name:       chunk.ToolCallName,
			}
			emitModelEvent(ch, ToolCallStarted{
				ID:    chunk.ToolCallID,
				Name:  chunk.ToolCallName,
				Index: chunk.Index,
			})
		}
		if chunk.Detail == "input_json_delta" && chunk.ToolCallInput != "" {
			if ts, ok := toolInputStates[chunk.Index]; ok {
				ts.input.WriteString(chunk.ToolCallInput)
				emitModelEvent(ch, ToolCallDelta{
					ID:    ts.id,
					Index: chunk.Index,
					Input: chunk.ToolCallInput,
				})
			}
		}
		if chunk.EventType == "content_block_stop" {
			if ts, ok := toolInputStates[chunk.Index]; ok {
				delete(toolInputStates, chunk.Index)
				raw := json.RawMessage(ts.input.String())
				// Log suspiciously empty inputs for debugging (don't skip — let
				// validateInput catch them so the model receives a proper error).
				inputStr := strings.TrimSpace(ts.input.String())
				if inputStr == "" || inputStr == "{}" || inputStr == "null" {
					logger.Debug("tool call with empty input detected", "name", ts.name, "id", ts.id)
				}
				resp.toolCalls = append(resp.toolCalls, ApiToolUseBlock{
					ID:         ts.id,
					ProviderID: ts.providerID,
					Name:       ts.name,
					Input:      raw,
				})
				emitModelEvent(ch, ToolCallCompleted{
					ID:    ts.id,
					Name:  ts.name,
					Input: raw,
					Index: chunk.Index,
				})
			}
		}

		// Thinking block assembly
		if chunk.EventType == "content_block_start" && chunk.IsThinking {
			thinkingIndex = chunk.Index
			thinkingProviderID = chunk.ThinkingProviderID
			thinkingSummaryText = chunk.ThinkingSummaryText
			thinkingBuf.Reset()
			emitModelEvent(ch, ThinkingStarted{Index: chunk.Index})
		}
		if chunk.EventType == "content_block_start" && chunk.IsRedactedThinking {
			redactedThinkingIndex = chunk.Index
		}
		if chunk.Detail == "thinking_delta" && chunk.ThinkingDelta != "" {
			thinkingBuf.WriteString(chunk.ThinkingDelta)
			emitModelEvent(ch, ThinkingDelta{Text: chunk.ThinkingDelta})
		}
		if chunk.EventType == "content_block_stop" && thinkingIndex >= 0 && chunk.Index == thinkingIndex {
			fullThinking := thinkingBuf.String()
			sig := chunk.ThinkingSignature
			pid := thinkingProviderID
			stext := thinkingSummaryText
			thinkingIndex = -1
			thinkingProviderID = ""
			thinkingSummaryText = ""
			thinkingBuf.Reset()
			resp.thinkingBlocks = append(resp.thinkingBlocks, ApiContentBlock{
				Type: ApiThinkingContentType,
				Thinking: &ApiThinkingBlock{
					ID:          pid,
					Text:        fullThinking,
					Signature:   sig,
					SummaryText: stext,
				},
			})
			emitModelEvent(ch, ThinkingCompleted{Text: fullThinking, Signature: sig})
		}
		if chunk.EventType == "content_block_stop" && redactedThinkingIndex >= 0 && chunk.Index == redactedThinkingIndex {
			resp.thinkingBlocks = append(resp.thinkingBlocks, ApiContentBlock{
				Type: ApiRedactedThinkingContentType,
				Thinking: &ApiThinkingBlock{
					Signature: chunk.ThinkingSignature,
				},
			})
			redactedThinkingIndex = -1
		}

		// Text delta (excludes thinking_delta which is routed above)
		if chunk.Delta != "" && chunk.Detail != "thinking_delta" {
			if !assistantStarted {
				emitModelEvent(ch, AssistantStarted{})
				assistantStarted = true
			}
			textBuf.WriteString(chunk.Delta)
			emitModelEvent(ch, AssistantDelta{Text: chunk.Delta})
		}

		if chunk.Done {
			resp.textContent = textBuf.String()
			var callNames []string
			for _, tc := range resp.toolCalls {
				callNames = append(callNames, tc.Name)
			}
			emitModelEvent(ch, StreamCompleted{
				InputTokens:     resp.inputTokens,
				OutputTokens:    resp.outputTokens,
				CacheReadTokens: resp.cacheReadTokens,
				StopReason:      resp.stopReason,
				Duration:        time.Since(start),
				ToolCalls:       callNames,
			})
			return resp, nil
		}
	}

	return modelResponse{}, errors.New("stream ended without message_stop")
}

func emitModelEvent(ch chan<- Event, ev Event) {
	if ch == nil {
		return
	}
	ch <- ev
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isRecoverableProviderError checks whether an error from a provider API stream
// is a recoverable parameter/validation error (as opposed to auth, network,
// or other fatal errors). These errors should not crash the agent loop —
// instead they are surfaced as text so the model can self-correct.
//
// Covers:
//   - codebase: trae_permanent_error, code=4001, ErrParamInvalid
//   - aiden:    InvalidParameter, required field, 400 Bad Request
//   - context:  prompt_too_long, input_too_long, context_length_exceeded
func isRecoverableProviderError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Context window exceeded — tell the model to compact
	if isContextTooLongError(msg) {
		return true
	}

	// Codebase API parameter errors. Message protocol errors must remain fatal;
	// recovering them as assistant text pollutes history and causes retry loops.
	if strings.Contains(msg, "codebase api error") {
		if strings.Contains(strings.ToLower(msg), "invalid message") {
			return false
		}
		if strings.Contains(msg, "code=4001") ||
			strings.Contains(msg, "ErrParamInvalid") ||
			strings.Contains(msg, "invalid param") ||
			strings.Contains(msg, "trae_permanent_error") {
			return true
		}
	}

	// Aiden API parameter errors (400 Bad Request with validation messages)
	if strings.Contains(msg, "aiden api returned") &&
		(strings.Contains(msg, "InvalidParameter") ||
			strings.Contains(msg, "required field") ||
			strings.Contains(msg, "invalid_parameter")) {
		return true
	}

	return false
}

// isContextTooLongError checks whether the error indicates the input exceeded
// the model's context window. Different providers use different error messages.
func isContextTooLongError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "prompt_too_long") ||
		strings.Contains(lower, "input_too_long") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "token_limit_exceeded") ||
		strings.Contains(lower, "max context") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "request too large")
}
