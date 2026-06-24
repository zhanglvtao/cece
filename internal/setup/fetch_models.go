package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchModels returns available models for a provider.
func fetchModels(protocol, baseURL, apiKey string) ([]modelOption, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/models"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	switch protocol {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		// aiden, codebase and other OpenAI-compatible providers
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("User-Agent", "cece-setup")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(envelope.Data) == 0 {
		return nil, fmt.Errorf("no models available")
	}

	models := make([]modelOption, len(envelope.Data))
	for i, m := range envelope.Data {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		models[i] = modelOption{id: m.ID, name: name}
	}
	return models, nil
}
