package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"cece/internal/chat"
	"cece/internal/logger"
	"cece/internal/tool"
)

// AuthMode controls how the API key is sent in request headers.
type AuthMode int

const (
	AuthModeAPIKey AuthMode = iota // x-api-key header (Anthropic native)
	AuthModeBearer                 // Authorization: Bearer header (proxy gateway)
)

// ParseAuthMode converts a string auth mode to an AuthMode value.
// Returns AuthModeAPIKey for empty string or "apikey", AuthModeBearer for "bearer".
func ParseAuthMode(s string) AuthMode {
	switch strings.ToLower(s) {
	case "bearer":
		return AuthModeBearer
	default:
		return AuthModeAPIKey
	}
}

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	authMode   AuthMode
	tokenCache *TokenCache // nil = use static apiKey
	httpClient *http.Client
}

func NewClient(apiKey, model, baseURL string, authMode AuthMode) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		authMode:   authMode,
		httpClient: &http.Client{},
	}
}

// SetAuthHelper configures dynamic token fetching via an external command.
// When set, the helper is called before each request to obtain a fresh token.
func (c *Client) SetAuthHelper(helper string) {
	c.tokenCache = NewTokenCache(helper, 0)
}

func (c *Client) SetModel(model string) { c.model = model }
func (c *Client) Model() string         { return c.model }
func (c *Client) SetProvider(apiKey, baseURL string, authMode int) {
	c.apiKey = apiKey
	c.baseURL = baseURL
	c.authMode = AuthMode(authMode)
}

// resolveAPIKey returns the current API key, refreshing via authHelper if needed.
func (c *Client) resolveAPIKey(ctx context.Context) (string, error) {
	if c.tokenCache != nil {
		return c.tokenCache.GetToken(ctx)
	}
	return c.apiKey, nil
}

// setAuthHeaders sets authentication and version headers based on the auth mode.
// Resolves the API key dynamically if an authHelper is configured.
func (c *Client) setAuthHeaders(ctx context.Context, h http.Header) error {
	h.Set("content-type", "application/json")
	h.Set("anthropic-version", "2023-06-01")
	key, err := c.resolveAPIKey(ctx)
	if err != nil {
		return fmt.Errorf("resolve api key: %w", err)
	}
	switch c.authMode {
	case AuthModeBearer:
		h.Set("Authorization", "Bearer "+key)
	default:
		h.Set("x-api-key", key)
	}
	return nil
}

func (c *Client) Stream(ctx context.Context, messages []chat.Message, system chat.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan chat.ApiStreamEvent, error) {
	// Build system blocks for Anthropic wire format
	var systemBlocks []any
	for _, block := range system.Blocks {
		entry := map[string]any{
			"type": "text",
			"text": block.Text,
		}
		if block.CacheControl != nil {
			entry["cache_control"] = block.CacheControl
		}
		systemBlocks = append(systemBlocks, entry)
	}

	payload := struct {
		Model     string            `json:"model"`
		MaxTokens int               `json:"max_tokens"`
		Stream    bool              `json:"stream"`
		System    []any             `json:"system,omitempty"`
		Messages  []any             `json:"messages"`
		Tools     []tool.Definition `json:"tools,omitempty"`
	}{
		Model:     c.model,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    systemBlocks,
		Tools:     tools,
	}

	for _, message := range messages {
		payload.Messages = append(payload.Messages, serializeMessage(message))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	messagesURL := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
	logger.Debug("api request body", "url", messagesURL, "body", string(body))
	slog.Info("stream request", "url", messagesURL, "model", c.model, "messages", len(payload.Messages))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if err := c.setAuthHeaders(ctx, req.Header); err != nil {
		return nil, fmt.Errorf("set auth headers: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("stream request failed", "error", err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		slog.Error("stream api error", "status", resp.Status, "body", strings.TrimSpace(string(raw)))
		return nil, fmt.Errorf("anthropic api returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	slog.Info("stream connected", "status", resp.StatusCode)
	return decodeStreamEvent(resp.Body), nil
}

// serializeMessage converts a chat.Message into the Anthropic wire format.
// Supports: plain text, ContentBlocks (text + tool_use), and tool_result.
func serializeMessage(m chat.Message) any {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type toolUseBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type toolResultBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error,omitempty"`
	}

	// tool_result messages (user role with ToolResult blocks)
	if m.Role == chat.UserRole && len(m.ContentBlocks) > 0 {
		if _, ok := m.ContentBlocks[0].AsToolResult(); ok {
			var blocks []any
			for _, cb := range m.ContentBlocks {
				if tr, ok := cb.AsToolResult(); ok {
					blocks = append(blocks, toolResultBlock{
						Type:      "tool_result",
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
						IsError:   tr.IsError,
					})
				}
			}
			return map[string]any{
				"role":    string(m.Role),
				"content": blocks,
			}
		}
	}

	// ApiContentBlocks (assistant with text + tool_use)
	if len(m.ContentBlocks) > 0 {
		var blocks []any
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case chat.ApiTextContentType:
				blocks = append(blocks, textBlock{Type: "text", Text: cb.Text})
			case chat.ApiToolUseContentType:
				if cb.ToolUse != nil {
					blocks = append(blocks, toolUseBlock{
						Type:  "tool_use",
						ID:    cb.ToolUse.ID,
						Name:  cb.ToolUse.Name,
						Input: cb.ToolUse.Input,
					})
				}
			}
		}
		return map[string]any{
			"role":    string(m.Role),
			"content": blocks,
		}
	}

	// Plain text fallback
	return map[string]any{
		"role":    string(m.Role),
		"content": m.Content,
	}
}
