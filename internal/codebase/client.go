package codebase

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

const cocoBusinessID = "coco-instance"

type Client struct {
	apiKey     string
	model      string
	configName string
	baseURL    string
	tokenCache *auth.TokenCache
	httpClient *http.Client
}

func NewClient(apiKey, model, configName, baseURL string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		configName: configName,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) SetAuthHelper(helper string) {
	c.tokenCache = auth.NewTokenCache(helper, 0)
}

func (c *Client) SetModel(model string)    { c.model = model }
func (c *Client) Model() string            { return c.model }
func (c *Client) SetConfigName(name string) { c.configName = name }

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

func (c *Client) Stream(ctx context.Context, messages []chat.Message, system chat.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan chat.ApiStreamEvent, error) {
	payload := CodebaseRequest{
		Model:      c.model,
		ConfigName: c.configName,
		Messages:   SerializeMessages(messages, system),
		Stream:     true,
		MaxTokens:  maxTokens,
	}

	if len(tools) > 0 {
		payload.Tools = ConvertTools(tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(c.baseURL, "/") + "/context/chat/completions"
	logger.Debug("codebase api request", "url", url, "body", string(body))
	slog.Info("codebase stream request", "url", url, "model", c.model, "config_name", c.configName, "messages", len(payload.Messages))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("X-Coco-Business-ID", cocoBusinessID)
	key, err := c.resolveAPIKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve api key: %w", err)
	}
	h.Set("Authorization", "Bearer "+key)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("codebase stream request failed", "error", err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		slog.Error("codebase stream api error", "status", resp.Status, "body", strings.TrimSpace(string(raw)))
		return nil, fmt.Errorf("codebase api returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	slog.Info("codebase stream connected", "status", resp.StatusCode)
	return DecodeStreamEvent(resp.Body), nil
}
