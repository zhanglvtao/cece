package codebase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/auth"
	"github.com/zhanglvtao/cece/internal/diag"
	"github.com/zhanglvtao/cece/internal/httpretry"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/tool"
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
	models     []agent.ModelInfo
	tokenCache *auth.TokenCache
	httpClient *http.Client
}

func NewClient(apiKey, model, configName, baseURL string) *Client {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:     apiKey,
		model:      model,
		configName: configName,
		baseURL:    baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // overall request timeout (stream reads reset per event)
		},
	}
}

func (c *Client) SetAuthHelper(helper string) {
	c.tokenCache = auth.NewTokenCache(helper, 0)
}

func (c *Client) SetModel(model string) { c.model = model }
func (c *Client) Model() string         { return c.model }

// SetReasoningEffort is a no-op for the codebase client.
func (c *Client) SetReasoningEffort(effort string) {}

func (c *Client) SetConfigName(name string) { c.configName = name }

func (c *Client) SetProvider(apiKey, baseURL string, _ int) {
	c.apiKey = apiKey
	if baseURL = normalizeBaseURL(baseURL); baseURL != "" {
		c.baseURL = baseURL
	}
}

func (c *Client) SetModels(models []agent.ModelInfo) {
	c.models = append([]agent.ModelInfo(nil), models...)
}

func (c *Client) ListModels(ctx context.Context) ([]agent.ModelInfo, error) {
	_ = ctx
	if len(c.models) > 0 {
		return append([]agent.ModelInfo(nil), c.models...), nil
	}
	models, err := DiscoverCocoPluginModels()
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (c *Client) GetModelInfo(ctx context.Context) (agent.ModelInfo, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return agent.ModelInfo{}, err
	}
	for _, m := range models {
		if m.ID == c.model || m.ConfigName == c.model || m.ConfigName == c.configName {
			return m, nil
		}
	}
	return agent.ModelInfo{}, fmt.Errorf("model %q not found in codebase models", c.model)
}

func (c *Client) resolveAPIKey(ctx context.Context, defaultKey string) (string, error) {
	// Only use defaultKey if it doesn't look like a macro
	if defaultKey != "" && !strings.Contains(defaultKey, "${") {
		return defaultKey, nil
	}
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

func expandHeaderValue(ctx context.Context, value string, c *Client) string {
	if !strings.Contains(value, "${") {
		return value
	}

	// Helper to resolve the key only if needed
	keyResolved := false
	var key string
	getKey := func() string {
		if !keyResolved {
			var err error
			// If APIKey is configured in the model, use it as default
			var defaultKey string
			if c != nil {
				for _, m := range c.models {
					if m.ID == c.model || m.ConfigName == c.model || m.ConfigName == c.configName {
						defaultKey = m.APIKey
						break
					}
				}
			}
			key, err = c.resolveAPIKey(ctx, defaultKey)
			if err != nil {
				logger.Debug("failed to resolve api key for macro expansion", "error", err)
			}
			keyResolved = true
		}
		return key
	}

	// Expand standard coco variables
	jwtValue := getKey()
	value = strings.ReplaceAll(value, "${CODE_USER_JWT}", jwtValue)
	value = strings.ReplaceAll(value, "${LOGID}", uuid.New().String())
	value = strings.ReplaceAll(value, "${SESSION_ID}", uuid.New().String()) // In real impl, pass actual session ID
	value = strings.ReplaceAll(value, "${COCO_BUSINESS_ID}", cocoBusinessID)
	value = strings.ReplaceAll(value, "${COCO_VERSION}", "cece-agent")
	value = strings.ReplaceAll(value, "${BATCH_MODE}", "false")

	repoURL := os.Getenv("REPO_URL")
	if repoURL == "" {
		repoURL = "unknown"
	}
	value = strings.ReplaceAll(value, "${REPO_URL}", repoURL)

	return value
}

func (c *Client) Stream(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	diag.Log("[DIAG] codebase.Stream() ENTERED model=%s config=%s messages=%d tools=%d max_tokens=%d", c.model, c.configName, len(messages), len(tools), maxTokens)
	slog.Info("codebase.Stream: entered", "model", c.model, "config_name", c.configName, "messages_count", len(messages), "tools_count", len(tools), "max_tokens", maxTokens)

	projectedMessages := agent.ProjectMessagesForRequest(messages)
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

	key, err := c.resolveAPIKey(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("resolve api key: %w", err)
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("codebase api key is empty; set apiKey or authHelper (for coco use %q)", DefaultAuthHelper)
	}

	url := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	logger.Debug("codebase api request", "url", url, "body", string(body))
	slog.Info("codebase stream request", "url", url, "model", c.model, "config_name", c.configName, "messages", len(payload.Messages))

	var modelInfo agent.ModelInfo
	for _, m := range c.models {
		if m.ID == c.model || m.ConfigName == c.model || m.ConfigName == c.configName {
			modelInfo = m
			break
		}
	}

	makeRequest := func() (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		h := r.Header
		h.Set("Content-Type", "application/json")
		h.Set("User-Agent", ceceUserAgent)

		// Expand and apply headers from modelInfo
		if modelInfo.Headers != nil {
			for k, v := range modelInfo.Headers {
				expanded := expandHeaderValue(ctx, v, c)
				h.Set(k, expanded)
			}
			if modelInfo.APIKey != "" {
				expanded := expandHeaderValue(ctx, modelInfo.APIKey, c)
				if !strings.HasPrefix(expanded, "Bearer ") && expanded != "" {
					h.Set("Authorization", "Bearer "+expanded)
				} else if expanded != "" {
					h.Set("Authorization", expanded)
				}
			}
			if h.Get("Authorization") == "" || h.Get("Authorization") == "Bearer " {
				// Fallback if macro expansion yielded empty
				key, _ := c.resolveAPIKey(ctx, "")
				h.Set("Authorization", "Bearer "+key)
			}
		} else {
			h.Set("X-Coco-Business-ID", cocoBusinessID)
			key, err := c.resolveAPIKey(ctx, "")
			if err != nil {
				return nil, fmt.Errorf("resolve api key: %w", err)
			}
			h.Set("Authorization", "Bearer "+key)
		}

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
		diag.Log("[DIAG] codebase.Stream: doRequest FAILED err=%v", err)
		slog.Warn("codebase.Stream: doRequest failed", "error", err)
		return nil, err
	}
	diag.Log("[DIAG] codebase.Stream: doRequest OK, starting decode goroutine")
	slog.Info("codebase stream connected", "status", 200)

	out := make(chan agent.ApiStreamEvent, 64)
	go func() {
		defer close(out)
		slog.Info("codebase.Stream: decode goroutine started")

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
						out <- agent.ApiStreamEvent{Err: err}
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
