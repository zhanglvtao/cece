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
	Role          Role
	Content       string // plain text, kept for backward compat
	ContentBlocks []ApiContentBlock
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
	ApiTextContentType       ApiContentBlockType = "text"
	ApiThinkingContentType   ApiContentBlockType = "thinking"
	ApiToolUseContentType    ApiContentBlockType = "tool_use"
	ApiToolResultContentType ApiContentBlockType = "tool_result"
)

type ApiContentBlock struct {
	Type       ApiContentBlockType
	Text       string              // for text blocks
	ToolUse    *ApiToolUseBlock    // for tool_use blocks
	ToolResult *ApiToolResultBlock // for tool_result blocks
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
	ToolUseID string // 对应 tool_use 的 ID
	Content   string // 工具执行结果文本
	IsError   bool   // 是否为错误结果
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
	IsThinking    bool   // true when content_block_start has type "thinking"
	ThinkingDelta string // text from thinking_delta
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
	Protocol         string // "anthropic" (default) or "openai" or "codebase"
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
