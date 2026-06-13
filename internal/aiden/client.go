package aiden

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zhanglvtao/cece/internal/auth"
	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/httpretry"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/tool"
)

const ceceUserAgent = "cece-agent"

type Client struct {
	apiKey          string
	model           string
	baseURL         string
	tokenCache      *auth.TokenCache
	httpClient      *http.Client
	reasoningEffort string
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) SetAuthHelper(helper string) {
	c.tokenCache = auth.NewTokenCache(helper, 0)
}

func (c *Client) SetModel(model string) { c.model = model }
func (c *Client) Model() string         { return c.model }

// SetReasoningEffort sets the reasoning effort for future requests.
func (c *Client) SetReasoningEffort(effort string) { c.reasoningEffort = effort }

func (c *Client) SetProvider(apiKey, baseURL string, _ int) {
	c.apiKey = apiKey
	c.baseURL = baseURL
}

func (c *Client) resolveAPIKey(ctx context.Context) (string, error) {
	if c.tokenCache != nil {
		return c.tokenCache.GetToken(ctx)
	}
	return c.apiKey, nil
}

func (c *Client) setAuth(req *http.Request) error {
	key, err := c.resolveAPIKey(req.Context())
	if err != nil {
		return fmt.Errorf("resolve api key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	return nil
}

func (c *Client) doRequestWithRetry(ctx context.Context, method, url string, body []byte, extraHeaders map[string]string) (*http.Response, error) {
	makeRequest := func() (*http.Request, error) {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("content-type", "application/json")
		}
		req.Header.Set("User-Agent", ceceUserAgent)
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		if err := c.setAuth(req); err != nil {
			return nil, err
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
		slog.Error("aiden api error", "status", resp.Status, "body", strings.TrimSpace(string(raw)), "request_id", reqID)
		errMsg := fmt.Sprintf("aiden api returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
		if reqID != "" {
			errMsg += " (" + reqID + ")"
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return resp, nil
}

func extractRequestID(resp *http.Response) string {
	if id := resp.Header.Get("x-request-id"); id != "" {
		return "request_id=" + id
	}
	return ""
}

func (c *Client) Stream(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	projectedMessages := agent.ProjectMessagesForRequest(messages)
	if usesResponsesAPI(c.model) {
		return c.streamResponses(ctx, projectedMessages, system, tools, maxTokens)
	}

	payload := ChatCompletionRequest{
		Model:           c.model,
		Messages:        SerializeMessages(projectedMessages, system),
		Stream:          true,
		MaxTokens:       maxTokens,
		StreamOptions:   &StreamOptions{IncludeUsage: true},
		ReasoningEffort: c.reasoningEffort,
	}

	if len(tools) > 0 {
		payload.Tools = ConvertTools(tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/v1/chat/completions"
	logger.Debug("aiden api request", "url", url, "body", string(body))
	slog.Info("aiden stream request", "url", url, "model", c.model, "messages", len(payload.Messages))

	resp, err := c.doRequestWithRetry(ctx, http.MethodPost, url, body, nil)
	if err != nil {
		return nil, err
	}

	slog.Info("aiden stream connected", "status", resp.StatusCode)
	return DecodeStreamEvent(resp.Body), nil
}

func (c *Client) streamResponses(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	payload := ResponsesRequest{
		Model:           c.model,
		Instructions:    serializeSystemInstructions(system),
		Input:           SerializeResponsesInput(messages),
		Stream:          true,
		MaxOutputTokens: maxTokens,
		ReasoningEffort: c.reasoningEffort,
	}

	if len(tools) > 0 {
		payload.Tools = ConvertResponsesTools(tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/v1/responses"
	logger.Debug("aiden responses api request", "url", url, "body", string(body))
	slog.Info("aiden responses stream request", "url", url, "model", c.model, "input", len(payload.Input))

	resp, err := c.doRequestWithRetry(ctx, http.MethodPost, url, body, nil)
	if err != nil {
		return nil, err
	}

	slog.Info("aiden responses stream connected", "status", resp.StatusCode)
	return DecodeStreamEvent(resp.Body), nil
}

func usesResponsesAPI(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt")
}

type aidenModel struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	OwnedBy          string `json:"owned_by"`
	Created          int64  `json:"created"`
	MaxInputTokens   int    `json:"max_input_tokens"`
	ContextLength    int    `json:"context_length"`
	MaxContextWindow int    `json:"max_context_window"`
}

func (m aidenModel) toChat() agent.ModelInfo {
	cw := m.MaxInputTokens
	if cw <= 0 {
		cw = m.ContextLength
	}
	if cw <= 0 {
		cw = m.MaxContextWindow
	}

	displayName := m.DisplayName
	if displayName == "" {
		displayName = m.ID
	}

	return agent.ModelInfo{
		ID:               m.ID,
		DisplayName:      displayName,
		MaxContextWindow: cw,
	}
}

func (c *Client) ListModels(ctx context.Context) ([]agent.ModelInfo, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/v1/models"

	resp, err := c.doRequestWithRetry(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var envelope struct {
		Data []aidenModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode list models response: %w", err)
	}

	result := make([]agent.ModelInfo, len(envelope.Data))
	for i, m := range envelope.Data {
		result[i] = m.toChat()
	}
	slog.Info("aiden models listed", "count", len(result))
	return result, nil
}

func (c *Client) GetModelInfo(ctx context.Context) (agent.ModelInfo, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return agent.ModelInfo{}, err
	}
	for _, m := range models {
		if m.ID == c.model {
			return m, nil
		}
	}
	return agent.ModelInfo{}, fmt.Errorf("model %q not found in listing", c.model)
}
