package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/logger"
)

func decodeStreamEvent(body io.ReadCloser) <-chan agent.ApiStreamEvent {
	out := make(chan agent.ApiStreamEvent)

	go func() {
		defer close(out)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		var dataLines []string

		flush := func() bool {
			if len(dataLines) == 0 {
				return true
			}

			payload := strings.Join(dataLines, "\n")
			dataLines = nil

			logger.Debug("sse flush", "payload", payload)

			var envelope struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Message struct {
					Usage struct {
						InputTokens         int `json:"input_tokens"`
						CacheCreationTokens int `json:"cache_creation_input_tokens"`
						CacheReadTokens     int `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Signature string `json:"signature"` // thinking block signature from content_block_stop
			}

			if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
				out <- agent.ApiStreamEvent{Err: err}
				return false
			}

			switch envelope.Type {
			case "message_start":
				out <- agent.ApiStreamEvent{
					EventType:           "message_start",
					InputTokens:         envelope.Message.Usage.InputTokens,
					CacheCreationTokens: envelope.Message.Usage.CacheCreationTokens,
					CacheReadTokens:     envelope.Message.Usage.CacheReadTokens,
				}
			case "content_block_start":
				if envelope.ContentBlock.Type == "tool_use" {
					out <- agent.ApiStreamEvent{
						EventType:    "content_block_start",
						ToolCallID:   envelope.ContentBlock.ID,
						ToolCallName: envelope.ContentBlock.Name,
						Index:        envelope.Index,
					}
				} else if envelope.ContentBlock.Type == "thinking" {
					out <- agent.ApiStreamEvent{
						EventType:  "content_block_start",
						Index:      envelope.Index,
						IsThinking: true,
					}
				} else if envelope.ContentBlock.Type == "redacted_thinking" {
					out <- agent.ApiStreamEvent{
						EventType:          "content_block_start",
						Index:              envelope.Index,
						IsRedactedThinking: true,
					}
				} else {
					// text block start — no actionable data yet
					out <- agent.ApiStreamEvent{
						EventType: "content_block_start",
						Index:     envelope.Index,
					}
				}
			case "content_block_delta":
				if envelope.Delta.Type == "input_json_delta" {
					out <- agent.ApiStreamEvent{
						EventType:     "content_block_delta",
						Detail:        "input_json_delta",
						ToolCallInput: envelope.Delta.PartialJSON,
						Index:         envelope.Index,
					}
				} else if envelope.Delta.Type == "thinking_delta" {
					out <- agent.ApiStreamEvent{
						EventType:     "content_block_delta",
						Detail:        "thinking_delta",
						ThinkingDelta: envelope.Delta.Thinking,
						Index:         envelope.Index,
					}
				} else if envelope.Delta.Text != "" {
					out <- agent.ApiStreamEvent{
						Delta:     envelope.Delta.Text,
						EventType: "content_block_delta",
						Detail:    envelope.Delta.Type,
						Index:     envelope.Index,
					}
				}
			case "content_block_stop":
				out <- agent.ApiStreamEvent{
					EventType:         "content_block_stop",
					Index:             envelope.Index,
					ThinkingSignature: envelope.Signature,
				}
			case "message_delta":
				out <- agent.ApiStreamEvent{
					EventType:    "message_delta",
					Detail:       "stop_reason",
					OutputTokens: envelope.Usage.OutputTokens,
					StopReason:   envelope.Delta.StopReason,
				}
			case "message_stop":
				out <- agent.ApiStreamEvent{Done: true}
				return false
			case "error":
				out <- agent.ApiStreamEvent{Err: errors.New(envelope.Error.Message)}
				return false
			}

			return true
		}

		for scanner.Scan() {
			line := scanner.Text()
			logger.Debug("sse raw line", "line", line)
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}

		if err := scanner.Err(); err != nil {
			out <- agent.ApiStreamEvent{Err: err}
			return
		}

		flush()
	}()

	return out
}
