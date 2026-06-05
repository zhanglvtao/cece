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

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/httpretry"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/tool"
)

// AuthMode controls how the API key is sent in request headers.
type AuthMode int

const (
	AuthModeAPIKey AuthMode = iota // x-api-key header (Anthropic native)
	AuthModeBearer                 // Authorization: Bearer header (proxy gateway)
)

const ceceUserAgent = "cece-agent"

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
	apiKey      string
	model       string
	baseURL     string
	authMode    AuthMode
	tokenCache  *TokenCache // nil = use static apiKey
	httpClient  *http.Client
	thinkingOn  bool // enable extended thinking
	thinkBudget int  // budget_tokens for thinking (0 = use default 10000)
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

// SetThinking enables extended thinking with the given budget (0 = default 10000).
func (c *Client) SetThinking(enabled bool, budget int) {
	c.thinkingOn = enabled
	c.thinkBudget = budget
}
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
	h.Set("User-Agent", ceceUserAgent)
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

func (c *Client) Stream(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	projectedMessages := agent.ProjectMessagesForRequest(messages)

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

	type thinkingConfig struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	}

	payload := struct {
		Model     string            `json:"model"`
		MaxTokens int               `json:"max_tokens"`
		Stream    bool              `json:"stream"`
		System    []any             `json:"system,omitempty"`
		Messages  []any             `json:"messages"`
		Tools     []tool.Definition `json:"tools,omitempty"`
		Thinking  *thinkingConfig   `json:"thinking,omitempty"`
	}{
		Model:     c.model,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    systemBlocks,
		Tools:     tools,
	}

	if c.thinkingOn {
		budget := c.thinkBudget
		if budget <= 0 {
			budget = 10000
		}
		payload.Thinking = &thinkingConfig{Type: "enabled", BudgetTokens: budget}
	}

	for _, message := range projectedMessages {
		payload.Messages = append(payload.Messages, serializeMessage(message))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	messagesURL := strings.TrimRight(c.baseURL, "/") + "/v1/messages"
	logger.Debug("api request body", "url", messagesURL, "body", string(body))
	slog.Info("stream request", "url", messagesURL, "model", c.model, "messages", len(payload.Messages))

	makeRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if err := c.setAuthHeaders(ctx, req.Header); err != nil {
			return nil, fmt.Errorf("set auth headers: %w", err)
		}
		return req, nil
	}

	var invalidate func()
	if c.tokenCache != nil {
		invalidate = c.tokenCache.Invalidate
	}

	resp, err := httpretry.DoWithAuthRefresh(ctx, c.httpClient, makeRequest, httpretry.Options{}, httpretry.AuthRefreshOptions{
		Invalidate: invalidate,
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		reqID := extractRequestID(resp)
		slog.Error("stream api error", "status", resp.Status, "body", strings.TrimSpace(string(raw)), "request_id", reqID)
		errMsg := fmt.Sprintf("anthropic api returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
		if reqID != "" {
			errMsg += " (" + reqID + ")"
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	slog.Info("stream connected", "status", resp.StatusCode)
	return decodeStreamEvent(resp.Body), nil
}

func extractRequestID(resp *http.Response) string {
	if id := resp.Header.Get("request-id"); id != "" {
		return "request_id=" + id
	}
	return ""
}

// serializeMessage converts a agent.Message into the Anthropic wire format.
// Supports: plain text, ContentBlocks (text + tool_use), and tool_result.
func serializeMessage(m agent.Message) any {
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
	if m.Role == agent.UserRole && len(m.ContentBlocks) > 0 {
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

	// ApiContentBlocks (assistant with thinking + text + tool_use)
	if len(m.ContentBlocks) > 0 {
		var blocks []any
		for _, cb := range m.ContentBlocks {
			switch cb.Type {
			case agent.ApiThinkingContentType:
				if cb.Thinking != nil {
					blocks = append(blocks, map[string]any{
						"type":      "thinking",
						"thinking":  cb.Thinking.Text,
						"signature": cb.Thinking.Signature,
					})
				}
			case agent.ApiRedactedThinkingContentType:
				if cb.Thinking != nil {
					blocks = append(blocks, map[string]any{
						"type":      "redacted_thinking",
						"signature": cb.Thinking.Signature,
					})
				}
			case agent.ApiTextContentType:
				blocks = append(blocks, textBlock{Type: "text", Text: cb.Text})
			case agent.ApiToolUseContentType:
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
