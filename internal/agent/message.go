package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"cece/internal/prompt"
	"cece/internal/tool"
)

type Role string

const (
	UserRole      Role = "user"
	AssistantRole Role = "assistant"
)

type Message struct {
	Role          Role              `json:"role"`
	Content       string            `json:"content,omitempty"`
	ContentBlocks []ApiContentBlock `json:"content_blocks,omitempty"`

	// CompactBoundary marks this message as a compact boundary marker.
	// When building API requests, only messages after the last boundary
	// (plus the boundary's summary) are sent to the model.
	CompactBoundary bool `json:"compact_boundary,omitempty"`
}

// TextContent returns the concatenated text from all text-type content blocks.
func (m Message) TextContent() string {
	if m.Content != "" {
		return m.Content
	}
	var b strings.Builder
	for _, block := range m.ContentBlocks {
		if block.Type == ApiTextContentType {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

type ApiContentBlockType string

const (
	ApiTextContentType             ApiContentBlockType = "text"
	ApiThinkingContentType         ApiContentBlockType = "thinking"
	ApiRedactedThinkingContentType ApiContentBlockType = "redacted_thinking"
	ApiToolUseContentType          ApiContentBlockType = "tool_use"
	ApiToolResultContentType       ApiContentBlockType = "tool_result"
)

type ApiContentBlock struct {
	Type       ApiContentBlockType `json:"type"`
	Text       string              `json:"text,omitempty"`
	Thinking   *ApiThinkingBlock   `json:"thinking,omitempty"`
	ToolUse    *ApiToolUseBlock    `json:"tool_use,omitempty"`
	ToolResult *ApiToolResultBlock `json:"tool_result,omitempty"`
}

type ApiThinkingBlock struct {
	Text      string `json:"thinking,omitempty"`
	Signature string `json:"signature"`
}

func (cb ApiContentBlock) AsToolResult() (*ApiToolResultBlock, bool) {
	if cb.Type == ApiToolResultContentType && cb.ToolResult != nil {
		return cb.ToolResult, true
	}
	return nil, false
}

type ApiToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ApiToolResultBlock struct {
	ToolUseID  string `json:"tool_use_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
}

// ApiStreamEvent represents a single event from the Anthropic SSE stream.
type ApiStreamEvent struct {
	Delta string
	Done  bool
	Err   error

	// SSE raw event details
	EventType           string // "message_start", "content_block_delta", etc.
	Detail              string // sub-type: "text_delta", "input_json_delta", "stop_reason", etc.
	InputTokens         int    // from message_start
	OutputTokens        int    // from message_delta
	StopReason          string // from message_delta: "end_turn", "tool_use", etc.
	CacheCreationTokens int    // from message_start usage
	CacheReadTokens     int    // from message_start usage

	// Tool call fields (from content_block_start + input_json_delta)
	ToolCallID    string // tool_use block id
	ToolCallName  string // tool_use block name
	ToolCallInput string // incremental JSON input (from input_json_delta)
	Index         int    // content block index

	// Thinking block fields (from content_block_start type="thinking" + thinking_delta)
	IsThinking         bool   // true when content_block_start has type "thinking"
	ThinkingDelta      string // text from thinking_delta
	ThinkingSignature  string // signature from content_block_stop
	IsRedactedThinking bool   // true when content_block_start has type "redacted_thinking"
}

type SystemPrompt struct {
	Blocks []SystemBlock
}

type SystemBlock struct {
	Text         string
	CacheControl map[string]string // nil = 不缓存
}

// ModelInfo holds model metadata (e.g. from the /v1/models API).
type ModelInfo struct {
	ID               string
	DisplayName      string
	MaxContextWindow int
	Provider         string // provider name this model belongs to
	APIKey           string // provider API key (for model switching)
	BaseURL          string // provider base URL (for model switching)
	AuthMode         string // "apikey" or "bearer"
	AuthHelper       string // shell command to fetch dynamic token
	Protocol         string // "anthropic" (default) or "aiden" or "codebase"
	ConfigName       string // codebase-api needs config_name
}

type ModelClient interface {
	Stream(ctx context.Context, messages []Message, system SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan ApiStreamEvent, error)
}

// AssembleResultToSystemPrompt converts a prompt.AssembleResult into a SystemPrompt
// for the Anthropic API, applying cache_control based on each segment's ContextLayer.
func AssembleResultToSystemPrompt(r prompt.AssembleResult) SystemPrompt {
	var blocks []SystemBlock
	for _, seg := range r.Segments {
		if seg.Content == "" {
			continue
		}
		blocks = append(blocks, SystemBlock{
			Text:         seg.Content,
			CacheControl: seg.Layer.CacheControl(),
		})
	}
	return SystemPrompt{Blocks: blocks}
}

func ProjectMessagesForRequest(messages []Message) []Message {
	// Only send messages from the last compact boundary onward.
	projected := make([]Message, len(messages))
	for i, message := range messages {
		projected[i] = projectMessageForRequest(message)
	}
	return projected
}

// MessagesAfterCompactBoundary returns messages from the last compact boundary
// onward (including the boundary's summary message). If no boundary exists,
// returns all messages. This ensures the API only sees post-compaction context.
func MessagesAfterCompactBoundary(messages []Message) []Message {
	boundaryIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].CompactBoundary {
			boundaryIdx = i
			break
		}
	}
	if boundaryIdx == -1 {
		return messages
	}
	return messages[boundaryIdx:]
}

// EnsureToolResultCoverage scans messages for orphaned tool_use blocks (assistant
// messages with tool calls that have no matching tool_result in subsequent user
// messages) and inserts synthetic tool_result messages immediately after each
// orphaned assistant message. This prevents API errors when a session is resumed
// after an interruption that left tool calls without results.
//
// Placement matters: OpenAI/codebase/aiden protocols require tool results to
// immediately follow the assistant message that made the tool call. Appending
// synthetic results at the end violates message ordering and causes
// "invalid params" errors from providers.
func EnsureToolResultCoverage(messages []Message) []Message {
	knownResults := make(map[string]struct{})
	for _, m := range messages {
		for _, cb := range m.ContentBlocks {
			if tr, ok := cb.AsToolResult(); ok {
				knownResults[tr.ToolUseID] = struct{}{}
			}
		}
	}

	// Build the result slice, inserting synthetic tool_result messages
	// immediately after each assistant message that contains orphaned tool_use blocks.
	var result []Message
	for _, m := range messages {
		result = append(result, m)

		if m.Role != AssistantRole {
			continue
		}

		var orphans []ApiToolUseBlock
		for _, cb := range m.ContentBlocks {
			if cb.Type == ApiToolUseContentType && cb.ToolUse != nil {
				if _, has := knownResults[cb.ToolUse.ID]; !has {
					orphans = append(orphans, *cb.ToolUse)
				}
			}
		}

		if len(orphans) == 0 {
			continue
		}

		synthetic := make([]ApiContentBlock, len(orphans))
		for i, tc := range orphans {
			synthetic[i] = ApiContentBlock{
				Type: ApiToolResultContentType,
				ToolResult: &ApiToolResultBlock{
					ToolUseID: tc.ID,
					Content:   "Tool call was interrupted and did not produce a result. You may retry this call if the result is still needed.",
					IsError:   true,
				},
			}
		}

		result = append(result, Message{
			Role:          UserRole,
			ContentBlocks: synthetic,
		})
	}

	return result
}

// ValidateToolResultCoverage is a safety-net check that verifies every tool_use
// block has a matching tool_result. If any orphans remain (which should not
// happen if EnsureToolResultCoverage was called), it patches them in-place.
// Returns the (possibly patched) message slice.
func ValidateToolResultCoverage(messages []Message) []Message {
	knownResults := make(map[string]struct{})
	for _, m := range messages {
		for _, cb := range m.ContentBlocks {
			if tr, ok := cb.AsToolResult(); ok {
				knownResults[tr.ToolUseID] = struct{}{}
			}
		}
	}

	var orphanCount int
	for _, m := range messages {
		if m.Role != AssistantRole {
			continue
		}
		for _, cb := range m.ContentBlocks {
			if cb.Type == ApiToolUseContentType && cb.ToolUse != nil {
				if _, has := knownResults[cb.ToolUse.ID]; !has {
					orphanCount++
				}
			}
		}
	}

	if orphanCount == 0 {
		return messages
	}

	slog.Warn("ValidateToolResultCoverage: found orphaned tool_use blocks, patching", "count", orphanCount)
	return EnsureToolResultCoverage(messages)
}

// TurnBoundaries returns the start index of each turn in messages.
// A turn starts at each user-role message. Returns slice of indices.
// Turn N spans messages[boundaries[N]:boundaries[N+1]] (or to len for the last turn).
func TurnBoundaries(messages []Message) []int {
	var boundaries []int
	for i, m := range messages {
		if m.Role == UserRole {
			boundaries = append(boundaries, i)
		}
	}
	return boundaries
}

// TrimToolResultsInRange trims tool_result content in messages belonging to
// turns [fromTurn, toTurn). Returns (trimmedCount, tokensBefore, tokensAfter).
// Mutates messages in place.
func TrimToolResultsInRange(messages []Message, fromTurn, toTurn int) (truncatedCount, tokensBefore, tokensAfter int) {
	boundaries := TurnBoundaries(messages)
	tokensBefore = EstimateMessagesTokens(messages)

	// Determine message index range for the specified turns
	msgStart := 0
	if fromTurn > 0 && fromTurn <= len(boundaries) {
		msgStart = boundaries[fromTurn-1]
	}
	// Note: fromTurn=0 means from the beginning, so msgStart=0

	msgEnd := len(messages)
	if toTurn <= len(boundaries) {
		msgEnd = boundaries[toTurn-1]
	}

	for i := msgStart; i < msgEnd && i < len(messages); i++ {
		for j := range messages[i].ContentBlocks {
			cb := &messages[i].ContentBlocks[j]
			if cb.Type == ApiToolResultContentType && cb.ToolResult != nil {
				if cb.ToolResult.Content != "[trimmed]" {
					cb.ToolResult.Content = "[trimmed]"
					cb.ToolResult.Truncated = true
					cb.ToolResult.TotalLines = 0
					truncatedCount++
				}
			}
		}
	}

	tokensAfter = EstimateMessagesTokens(messages)
	return
}

// PruneBeforeTurn deletes all messages before the given turn.
// Returns the pruned message list (starting from the turn's boundary)
// plus a CompactBoundary message summarizing what was removed.
func PruneBeforeTurn(messages []Message, turn int) ([]Message, int, int) {
	boundaries := TurnBoundaries(messages)
	tokensBefore := EstimateMessagesTokens(messages)

	if turn <= 0 || turn > len(boundaries) {
		return messages, tokensBefore, tokensBefore
	}

	startIdx := boundaries[turn-1]
	pruned := messages[startIdx:]

	// Count how many turns were pruned
	prunedMsgCount := startIdx
	prunedTurnCount := turn

	boundary := Message{
		Role: UserRole,
		Content: fmt.Sprintf(
			"Context pruned: %d messages across %d turns before this point have been removed to free context. Continue the conversation based on what remains.",
			prunedMsgCount, prunedTurnCount,
		),
		CompactBoundary: true,
	}

	result := append([]Message{boundary}, pruned...)
	tokensAfter := EstimateMessagesTokens(result)
	return result, tokensBefore, tokensAfter
}

func projectMessageForRequest(message Message) Message {
	projected := message
	if len(message.ContentBlocks) == 0 {
		return projected
	}

	if message.Role != AssistantRole {
		projected.ContentBlocks = append([]ApiContentBlock(nil), message.ContentBlocks...)
		return projected
	}

	blocks := make([]ApiContentBlock, 0, len(message.ContentBlocks))
	for _, block := range message.ContentBlocks {
		switch block.Type {
		case ApiThinkingContentType, ApiRedactedThinkingContentType:
			continue
		default:
			blocks = append(blocks, block)
		}
	}
	projected.ContentBlocks = blocks
	return projected
}
