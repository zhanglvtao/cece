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
	"cece/internal/httpretry"
	"cece/internal/logger"
	"cece/internal/tool"
)

const (
	cocoBusinessID = "coco-instance"
	ceceUserAgent  = "cece-agent"
)

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

func (c *Client) SetModel(model string)     { c.model = model }
func (c *Client) Model() string             { return c.model }
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

func extractRequestID(resp *http.Response) string {
	if id := resp.Header.Get("x-request-id"); id != "" {
		return "request_id=" + id
	}
	if id := resp.Header.Get("request-id"); id != "" {
		return "request_id=" + id
	}
	return ""
}

func (c *Client) Stream(ctx context.Context, messages []chat.Message, system chat.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan chat.ApiStreamEvent, error) {
	projectedMessages := chat.ProjectMessagesForRequest(messages)
	payload := CodebaseRequest{
		Model:      c.model,
		ConfigName: c.configName,
		Messages:   SerializeMessages(projectedMessages, system),
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

	url := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	logger.Debug("codebase api request", "url", url, "body", string(body))
	slog.Info("codebase stream request", "url", url, "model", c.model, "config_name", c.configName, "messages", len(payload.Messages))

	makeRequest := func() (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		h := r.Header
		h.Set("Content-Type", "application/json")
		h.Set("X-Coco-Business-ID", cocoBusinessID)
		h.Set("User-Agent", ceceUserAgent)
		key, err := c.resolveAPIKey(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve api key: %w", err)
		}
		h.Set("Authorization", "Bearer "+key)
		return r, nil
	}

	var invalidate func()
	if c.tokenCache != nil {
		invalidate = c.tokenCache.Invalidate
	}

	doRequest := func() (io.ReadCloser, error) {
		resp, err := httpretry.DoWithAuthRefresh(ctx, c.httpClient, makeRequest, httpretry.Options{}, httpretry.AuthRefreshOptions{
			Invalidate: invalidate,
		})
		if err != nil {
			slog.Error("codebase stream request failed", "error", err)
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			reqID := extractRequestID(resp)
			slog.Error("codebase stream api error", "status", resp.Status, "body", strings.TrimSpace(string(raw)), "request_id", reqID)
			errMsg := fmt.Sprintf("codebase api returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
			if reqID != "" {
				errMsg += " (" + reqID + ")"
			}
			return nil, fmt.Errorf("%s", errMsg)
		}
		return resp.Body, nil
	}

	reader, err := doRequest()
	if err != nil {
		return nil, err
	}

	slog.Info("codebase stream connected", "status", 200)

	out := make(chan chat.ApiStreamEvent)
	go func() {
		defer close(out)

		attempt := 0
		const maxRetries = 1

		innerReader := reader
		for {
			retried := false
			for event := range DecodeStreamEvent(innerReader) {
				if event.Err != nil && isCodebaseRetryable(event.Err) && attempt < maxRetries {
					slog.Warn("codebase stream retrying after retryable error", "model", c.model, "attempt", attempt+1)
					attempt++
					retried = true
					innerReader, err = doRequest()
					if err != nil {
						out <- chat.ApiStreamEvent{Err: err}
						return
					}
					slog.Info("codebase stream retry connected", "model", c.model, "attempt", attempt+1)
					break
				}
				out <- event
			}
			if !retried {
				return
			}
		}
	}()

	return out, nil
}
