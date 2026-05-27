package chat

import (
	"context"
	"encoding/json"
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
