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

	"github.com/zhanglvtao/cece/internal/agent"
	"github.com/zhanglvtao/cece/internal/auth"
	"github.com/zhanglvtao/cece/internal/httpretry"
	"github.com/zhanglvtao/cece/internal/logger"
	"github.com/zhanglvtao/cece/internal/tool"
)

const ceceUserAgent = "cece-agent"

type Client struct {
	apiKey          string
	model           string
	baseURL         string
	pathPrefix      string
	tokenCache      *auth.TokenCache
	tokenProvider   func(context.Context) (string, error)
	httpClient      *http.Client
	reasoningEffort string
	useResponsesAPI bool
}

func NewClient(apiKey, model, baseURL string) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		pathPrefix: "/v1",
		httpClient: &http.Client{},
	}
}

func (c *Client) SetAuthHelper(helper string) {
	c.tokenCache = auth.NewTokenCache(helper, 0)
}

func (c *Client) SetTokenProvider(provider func(context.Context) (string, error)) {
	c.tokenProvider = provider
}

func (c *Client) SetModel(model string) { c.model = model }
func (c *Client) Model() string         { return c.model }

// SetUseResponsesAPI enables the Responses API protocol (/v1/responses) instead
// of Chat Completions (/v1/chat/completions). This bypasses Aiden proxy's buggy
// g() conversion function which incorrectly marks assistant text as "input_text"
// instead of "output_text", causing 400 errors from OpenAI.
func (c *Client) SetUseResponsesAPI(v bool) { c.useResponsesAPI = v }

// SetPathPrefix configures the API path prefix inserted between baseURL and
// endpoint names. The default is "/v1" for OpenAI-compatible providers. Some
// traecli plugin base URLs point at an app root and expose /chat/completions
// directly under that root.
func (c *Client) SetPathPrefix(prefix string) { c.pathPrefix = strings.TrimRight(prefix, "/") }

// SetReasoningEffort sets the reasoning effort for future requests.
func (c *Client) SetReasoningEffort(effort string) { c.reasoningEffort = effort }

// mapAPIEffort maps cece-internal effort levels to API-compatible values.
// "xhigh" is cece's own level; the API only accepts low/medium/high.
func mapAPIEffort(effort string) string {
	if effort == "xhigh" {
		return "high"
	}
	return effort
}

func (c *Client) SetProvider(apiKey, baseURL string, _ int) {
	c.apiKey = apiKey
	c.baseURL = baseURL
}

func (c *Client) resolveAPIKey(ctx context.Context) (string, error) {
	if c.tokenProvider != nil {
		return c.tokenProvider(ctx)
	}
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

	// Log request info (enough to reproduce with curl)
	logger.Info("api request", "method", method, "url", url, "headers", redactAuthHeaders(extraHeaders, ceceUserAgent), "body", string(body))

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

	// Log response info
	logger.Info("api response", "status", resp.StatusCode, "headers", resp.Header)

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

// redactAuthHeaders returns a header map for logging, keeping structure but
// masking the Authorization value so credentials don't leak into logs.
func redactAuthHeaders(extra map[string]string, ua string) map[string]string {
	h := map[string]string{"User-Agent": ua}
	for k, v := range extra {
		h[k] = v
	}
	h["Authorization"] = "Bearer ****"
	return h
}

func extractRequestID(resp *http.Response) string {
	if id := resp.Header.Get("x-request-id"); id != "" {
		return "request_id=" + id
	}
	return ""
}

func (c *Client) Stream(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	if c.useResponsesAPI {
		return c.streamResponses(ctx, messages, system, tools, maxTokens)
	}
	return c.streamChatCompletions(ctx, messages, system, tools, maxTokens)
}

func (c *Client) streamResponses(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	projected := agent.ProjectMessagesForRequest(messages)
	input := SerializeResponsesInput(projected, system, c.model)
	instructions := SerializeResponsesInstructions(system)

	req := ResponsesAPIRequest{
		Model:           c.model,
		Input:           input,
		Instructions:    instructions,
		Stream:          true,
		MaxOutputTokens: maxTokens,
		Store:           true,
	}

	if len(tools) > 0 {
		req.Tools = ConvertResponsesTools(ConvertTools(tools))
	}

	if c.reasoningEffort != "" && isReasoningModel(c.model) {
		req.Reasoning = &ResponsesReasoning{
			Effort:  mapAPIEffort(c.reasoningEffort),
			Summary: "concise",
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := c.endpointURL("responses")

	resp, err := c.doRequestWithRetry(ctx, http.MethodPost, url, body, nil)
	if err != nil {
		return nil, err
	}

	return DecodeResponsesStream(resp.Body), nil
}

func (c *Client) streamChatCompletions(ctx context.Context, messages []agent.Message, system agent.SystemPrompt, tools []tool.Definition, maxTokens int) (<-chan agent.ApiStreamEvent, error) {
	payload := ChatCompletionRequest{
		Model:     c.model,
		Messages:  SerializeMessages(agent.ProjectMessagesForRequest(messages), system),
		Stream:    true,
		MaxTokens: maxTokens,
	}

	// Only send reasoning_effort for reasoning models (o1/o3/o4/gpt-5*).
	// Non-reasoning models don't support this field and may reject it.
	if c.reasoningEffort != "" && isReasoningModel(c.model) {
		payload.ReasoningEffort = mapAPIEffort(c.reasoningEffort)
	}

	if len(tools) > 0 {
		payload.Tools = ConvertTools(tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := c.endpointURL("chat/completions")

	resp, err := c.doRequestWithRetry(ctx, http.MethodPost, url, body, nil)
	if err != nil {
		return nil, err
	}

	return DecodeStreamEvent(resp.Body), nil
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
	url := c.endpointURL("models")

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

func (c *Client) endpointURL(endpoint string) string {
	base := strings.TrimRight(c.baseURL, "/")
	prefix := strings.Trim(c.pathPrefix, "/")
	endpoint = strings.TrimLeft(endpoint, "/")
	if prefix == "" {
		return base + "/" + endpoint
	}
	return base + "/" + prefix + "/" + endpoint
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
