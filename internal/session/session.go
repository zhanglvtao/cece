package session

import "time"

// StatusBarSnapshot holds cumulative status bar data for persistence across sessions.
type StatusBarSnapshot struct {
	APICalls            int            `json:"api_calls,omitempty"`
	ToolCounts          map[string]int `json:"tool_counts,omitempty"`
	CacheReadTokens     int            `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int            `json:"cache_creation_tokens,omitempty"`
	TurnCount           int            `json:"turn_count,omitempty"`
}

// Session holds metadata about a conversation session.
type Session struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Preview           string            `json:"preview,omitempty"`
	MessageCount      int               `json:"message_count,omitempty"`
	Model             string            `json:"model,omitempty"`
	ContextWindow     int               `json:"context_window,omitempty"`
	Protocol          string            `json:"protocol,omitempty"`
	ConfigName        string            `json:"config_name,omitempty"`
	LastInputTokens   int               `json:"last_input_tokens,omitempty"`
	TotalInputTokens  int               `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int               `json:"total_output_tokens,omitempty"`
	StatusBar         StatusBarSnapshot `json:"status_bar,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// SessionMeta holds mutable session metadata that updates on each model call.
type SessionMeta struct {
	Model             string
	ContextWindow     int
	Protocol          string
	ConfigName        string
	LastInputTokens   int
	TotalInputTokens  int
	TotalOutputTokens int
	StatusBar         StatusBarSnapshot
}
