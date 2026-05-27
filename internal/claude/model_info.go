package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"cece/internal/chat"
	"cece/internal/httpretry"
	"cece/internal/logger"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// apiModelInfo is the wire format for the /v1/models API.
// Supports both Anthropic (max_context_window) and OpenAI-compatible (context_length) fields.
type apiModelInfo struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	MaxContextWindow int    `json:"max_context_window"`
	ContextLength    int    `json:"context_length"`
}

func (a apiModelInfo) toChat() chat.ModelInfo {
	cw := a.MaxContextWindow
	if cw <= 0 {
		cw = a.ContextLength
	}
	return chat.ModelInfo{
		ID:               a.ID,
		DisplayName:      a.DisplayName,
		MaxContextWindow: cw,
	}
}

// GetModelInfo queries the Anthropic /v1/models/{model} endpoint for model metadata.
func (c *Client) GetModelInfo(ctx context.Context) (*chat.ModelInfo, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/v1/models/" + c.model

	makeRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create model info request: %w", err)
		}
		if err := c.setAuthHeaders(ctx, req.Header); err != nil {
			return nil, fmt.Errorf("set auth headers: %w", err)
		}
		return req, nil
	}

	resp, err := httpretry.Do(ctx, c.httpClient, makeRequest, httpretry.Options{})
	if err != nil {
		return nil, fmt.Errorf("model info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		bodyStr := strings.TrimSpace(string(raw))
		slog.Warn("model info api error", "status", resp.Status, "body", bodyStr)
		logger.Debug("api error response", "url", url, "status", resp.Status, "body", bodyStr)
		return nil, fmt.Errorf("model info api returned %s", resp.Status)
	}

	var envelope struct {
		Data apiModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode model info response: %w", err)
	}

	result := envelope.Data.toChat()
	slog.Info("model info retrieved", "model", result.ID, "max_context", result.MaxContextWindow)
	return &result, nil
}

// ListModels queries GET /v1/models for all available models.
func (c *Client) ListModels(ctx context.Context) ([]chat.ModelInfo, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/v1/models"

	makeRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create list models request: %w", err)
		}
		if err := c.setAuthHeaders(ctx, req.Header); err != nil {
			return nil, fmt.Errorf("set auth headers: %w", err)
		}
		return req, nil
	}

	resp, err := httpretry.Do(ctx, c.httpClient, makeRequest, httpretry.Options{})
	if err != nil {
		return nil, fmt.Errorf("list models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		bodyStr := strings.TrimSpace(string(raw))
		slog.Warn("list models api error", "status", resp.Status, "body", bodyStr)
		return nil, fmt.Errorf("list models api returned %s", resp.Status)
	}

	var envelope struct {
		Data []apiModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode list models response: %w", err)
	}

	result := make([]chat.ModelInfo, len(envelope.Data))
	for i, m := range envelope.Data {
		result[i] = m.toChat()
	}
	slog.Info("models listed", "count", len(result))
	return result, nil
}
