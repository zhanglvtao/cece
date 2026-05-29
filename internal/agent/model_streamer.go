package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cece/internal/logger"
	"cece/internal/tool"
)

// modelResponse holds the result of a single model stream invocation.
type modelResponse struct {
	stopReason        string
	inputTokens       int
	outputTokens      int
	toolCalls         []ApiToolUseBlock // non-empty when stopReason == "tool_use"
	textContent       string            // assistant text reply
	thinkingBlocks    []ApiContentBlock // thinking + redacted_thinking blocks
	cacheReadTokens   int               // cache read tokens from this stream
	cacheCreationTokens int             // cache creation tokens from this stream
}

// toolCallState tracks incremental assembly of a tool_use block across SSE events.
type toolCallState struct {
	id    string
	name  string
	input strings.Builder
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
	client        ModelClient
	registry      *tool.Registry
	onInputTokens func(int)
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
	ch <- ModelRequestStarted{
		Reason:               req.Reason,
		ToolResults:          req.ToolResults,
		EstimatedInputTokens: estimated,
	}

	chunks, err := s.client.Stream(ctx, req.Messages, req.System, tools, req.MaxTokens)
	if err != nil {
		return modelResponse{}, err
	}

	start := time.Now()

	var resp modelResponse
	var textBuf strings.Builder
	var thinkingBuf strings.Builder
	var thinkingIndex int = -1         // index of the current thinking block, -1 = none
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
				ch <- RunFailed{Err: chunk.Err}
				return modelResponse{
					stopReason:  "end_turn",
					textContent: fmt.Sprintf("[Provider Error] %s\nThe previous tool call had parameter issues that the provider rejected. You may retry.", chunk.Err.Error()),
				}, nil
			}
			return modelResponse{}, chunk.Err
		}

		if chunk.EventType != "" && !chunk.Done {
			ch <- StreamEventDetail{
				EventType: chunk.EventType,
				Detail:    chunk.Detail,
				Text:      truncate(chunk.Delta, 60),
			}
		}

		if chunk.EventType == "message_start" {
			resp.inputTokens = chunk.InputTokens
			if s.onInputTokens != nil {
				s.onInputTokens(resp.inputTokens)
			}
			resp.cacheReadTokens = chunk.CacheReadTokens
			resp.cacheCreationTokens = chunk.CacheCreationTokens
			var toolNames []string
			for _, def := range tools {
				toolNames = append(toolNames, def.Name)
			}
			ch <- StreamStarted{
				InputTokens:         resp.inputTokens,
				Tools:               toolNames,
				CacheCreationTokens: chunk.CacheCreationTokens,
				CacheReadTokens:     chunk.CacheReadTokens,
			}
		}
		if chunk.EventType == "message_delta" {
			resp.outputTokens = chunk.OutputTokens
			resp.stopReason = chunk.StopReason
		}

		// Tool use assembly
		if chunk.EventType == "content_block_start" && chunk.ToolCallID != "" {
			if toolInputStates == nil {
				toolInputStates = make(map[int]*toolCallState)
			}
			toolInputStates[chunk.Index] = &toolCallState{
				id:   chunk.ToolCallID,
				name: chunk.ToolCallName,
			}
			ch <- ToolCallStarted{
				ID:    chunk.ToolCallID,
				Name:  chunk.ToolCallName,
				Index: chunk.Index,
			}
		}
		if chunk.Detail == "input_json_delta" && chunk.ToolCallInput != "" {
			if ts, ok := toolInputStates[chunk.Index]; ok {
				ts.input.WriteString(chunk.ToolCallInput)
				ch <- ToolCallDelta{
					ID:    ts.id,
					Index: chunk.Index,
					Input: chunk.ToolCallInput,
				}
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
					ID:    ts.id,
					Name:  ts.name,
					Input: raw,
				})
				ch <- ToolCallCompleted{
					ID:    ts.id,
					Name:  ts.name,
					Input: raw,
					Index: chunk.Index,
				}
			}
		}

		// Thinking block assembly
		if chunk.EventType == "content_block_start" && chunk.IsThinking {
			thinkingIndex = chunk.Index
			thinkingBuf.Reset()
			ch <- ThinkingStarted{Index: chunk.Index}
		}
		if chunk.EventType == "content_block_start" && chunk.IsRedactedThinking {
			redactedThinkingIndex = chunk.Index
		}
		if chunk.Detail == "thinking_delta" && chunk.ThinkingDelta != "" {
			thinkingBuf.WriteString(chunk.ThinkingDelta)
			ch <- ThinkingDelta{Text: chunk.ThinkingDelta}
		}
		if chunk.EventType == "content_block_stop" && thinkingIndex >= 0 && chunk.Index == thinkingIndex {
			fullThinking := thinkingBuf.String()
			sig := chunk.ThinkingSignature
			thinkingIndex = -1
			thinkingBuf.Reset()
			resp.thinkingBlocks = append(resp.thinkingBlocks, ApiContentBlock{
				Type: ApiThinkingContentType,
				Thinking: &ApiThinkingBlock{
					Text:      fullThinking,
					Signature: sig,
				},
			})
			ch <- ThinkingCompleted{Text: fullThinking, Signature: sig}
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
				ch <- AssistantStarted{}
				assistantStarted = true
			}
			textBuf.WriteString(chunk.Delta)
			ch <- AssistantDelta{Text: chunk.Delta}
		}

		if chunk.Done {
			resp.textContent = textBuf.String()
			var callNames []string
			for _, tc := range resp.toolCalls {
				callNames = append(callNames, tc.Name)
			}
			ch <- StreamCompleted{
				OutputTokens: resp.outputTokens,
				StopReason:   resp.stopReason,
				Duration:     time.Since(start),
				ToolCalls:    callNames,
			}
			return resp, nil
		}
	}

	return modelResponse{}, errors.New("stream ended without message_stop")
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
func isRecoverableProviderError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Codebase API parameter errors
	if strings.Contains(msg, "codebase api error") &&
		(strings.Contains(msg, "code=4001") ||
			strings.Contains(msg, "ErrParamInvalid") ||
			strings.Contains(msg, "invalid param") ||
			strings.Contains(msg, "trae_permanent_error")) {
		return true
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
