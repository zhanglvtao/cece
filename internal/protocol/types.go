package protocol

import (
	"encoding/json"
	"time"
)

// ── Shared value types ─────────────────────────────────────────────────────
// dto 包是纯数据传输层，零依赖 internal 其他包。所有类型都是可 JSON 序列化的值类型。

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Question represents a single question from the LLM to the user.
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Options     []QuestionOption `json:"options"`
	Preview     string           `json:"preview,omitempty"`
}

// QuestionOption is a single choice for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionAnswer is the user's response to a question.
type QuestionAnswer struct {
	Question string   `json:"question"`
	Selected []string `json:"selected,omitempty"`
	Custom   string   `json:"custom,omitempty"`
}

// ModelInfo holds model metadata for UI display.
type ModelInfo struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name,omitempty"`
	MaxContextWindow int    `json:"max_context_window"`
	Provider         string `json:"provider,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	BaseURL          string `json:"base_url,omitempty"`
	AuthMode         string `json:"auth_mode,omitempty"`
	AuthHelper       string `json:"auth_helper,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	ConfigName       string `json:"config_name,omitempty"`
}

// Message represents a chat message for UI rendering.
type Message struct {
	Role          string         `json:"role"`
	Content       string         `json:"content,omitempty"`
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"`
}

// ContentBlockType identifies the type of a content block.
type ContentBlockType string

const (
	TextContentType       ContentBlockType = "text"
	ThinkingContentType   ContentBlockType = "thinking"
	ToolUseContentType    ContentBlockType = "tool_use"
	ToolResultContentType ContentBlockType = "tool_result"
)

// ContentBlock is a discriminated union of content block types.
type ContentBlock struct {
	Type       ContentBlockType `json:"type"`
	Text       string           `json:"text,omitempty"`
	ToolUse    *ToolUseBlock    `json:"tool_use,omitempty"`
	ToolResult *ToolResultBlock `json:"tool_result,omitempty"`
}

// ToolUseBlock represents a tool_use content block.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResultBlock represents a tool_result content block.
type ToolResultBlock struct {
	ToolUseID  string `json:"tool_use_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
}

// PermissionMode represents the current permission mode.
type PermissionMode string

const (
	PermissionModeDefault    PermissionMode = "default"
	PermissionModeAutoAccept PermissionMode = "auto-accept"
	PermissionModePlan       PermissionMode = "plan"
)

// SessionInfo holds session metadata for UI display.
type SessionInfo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}
