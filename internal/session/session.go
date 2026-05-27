package session

import "time"

// Session holds metadata about a conversation session.
type Session struct {
	ID                string    `json:"id"`
	Title             string    `json:"title"`
	Model             string    `json:"model,omitempty"`
	ContextWindow     int       `json:"context_window,omitempty"`
	Protocol          string    `json:"protocol,omitempty"`
	ConfigName        string    `json:"config_name,omitempty"`
	LastInputTokens   int       `json:"last_input_tokens,omitempty"`
	TotalInputTokens  int       `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int       `json:"total_output_tokens,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
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
}
