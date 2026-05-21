package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"cece/internal/auth"
	"cece/internal/chat"
	"cece/internal/logger"
	"cece/internal/tool"
)

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	tokenCache *auth.TokenCache
	httpClient *http.Client
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

// setAuth sets the Authorization header on req by resolving the API key
// (either from tokenCache or the static apiKey field).
func (c *Client) setAuth(req *http.Request) error {
	key, err := c.resolveAPIKey(req.Context())
	if err != nil {
		return fmt.Errorf("resolve api key: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	return nil
}

// doRequest builds a request, sets auth, executes it, and returns the response.
// Non-OK status codes result in an error (body is read and included).
func (c *Client) doRequest(ctx context.Context, method, url string, body []byte, extraHeaders map[string]string) (*http.Response, error) {
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
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	if err := c.setAuth(req); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		errBody := strings.TrimSpace(string(raw))
		return nil, fmt.Errorf("openai api returned %s: %s", resp.Status, errBody)
	}

	return resp, nil
}

// doRequestWithAuthRetry executes a request with automatic 401 retry logic.
// If the request returns 401 Unauthorized AND the client has a tokenCache,
// it invalidates the cached token, re-resolves the key, and retries once.
func (c *Client) doRequestWithAuthRetry(ctx context.Context, method, url string, body []byte, extraHeaders map[string]string) (*http.Response, error) {
	resp, err := c.doRequest(ctx, method, url, body, extraHeaders)
	if err == nil {
		return resp, nil
	}

	// Only retry on 401 when we have a tokenCache that can refresh the token
	if c.tokenCache != nil && strings.Contains(err.Error(), "401") {
		slog.Info("openai 401 detected, invalidating token cache and retrying")
		c.tokenCache.Invalidate()

		resp, retryErr := c.doRequest(ctx, method, url, body, extraHeaders)
		if retryErr != nil {
			return nil, retryErr
		}
		return resp, nil
	}

	return nil, err
}

func (c *Client) Stream(ctx context.Context, messages []chat.Message, system chat.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan chat.ApiStreamEvent, error) {
	payload := ChatCompletionRequest{
		Model:     c.model,
		Messages:  SerializeMessages(messages, system),
		Stream:    true,
		MaxTokens: maxTokens,
	}

	if len(tools) > 0 {
		payload.Tools = ConvertTools(tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/v1/chat/completions"
	logger.Debug("openai api request", "url", url, "body", string(body))
	slog.Info("openai stream request", "url", url, "model", c.model, "messages", len(payload.Messages))

	resp, err := c.doRequestWithAuthRetry(ctx, http.MethodPost, url, body, nil)
	if err != nil {
		return nil, err
	}

	slog.Info("openai stream connected", "status", resp.StatusCode)
	return DecodeStreamEvent(resp.Body), nil
}

type oaiModel struct {
	ID        string `json:"id"`
	OwnedBy   string `json:"owned_by"`
	Created   int64  `json:"created"`
}

func (c *Client) ListModels(ctx context.Context) ([]chat.ModelInfo, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/v1/models"

	resp, err := c.doRequestWithAuthRetry(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var envelope struct {
		Data []oaiModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode list models response: %w", err)
	}

	result := make([]chat.ModelInfo, len(envelope.Data))
	for i, m := range envelope.Data {
		result[i] = chat.ModelInfo{
			ID:          m.ID,
			DisplayName: m.ID,
		}
	}
	slog.Info("openai models listed", "count", len(result))
	return result, nil
}

func (c *Client) GetModelInfo(ctx context.Context) (chat.ModelInfo, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return chat.ModelInfo{}, err
	}
	for _, m := range models {
		if m.ID == c.model {
			return m, nil
		}
	}
	return chat.ModelInfo{}, fmt.Errorf("model %q not found in listing", c.model)
}
