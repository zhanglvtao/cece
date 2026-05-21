package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetModelInfoSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/claude-sonnet-4-6" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing or wrong api key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing anthropic-version header")
		}
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{
			"data": {
				"id": "claude-sonnet-4-6",
				"display_name": "Claude Sonnet 4.6",
				"max_context_window": 200000
			}
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)
	info, err := client.GetModelInfo(context.Background())
	if err != nil {
		t.Fatalf("GetModelInfo() error: %v", err)
	}
	if info.ID != "claude-sonnet-4-6" {
		t.Fatalf("ID = %q, want %q", info.ID, "claude-sonnet-4-6")
	}
	if info.MaxContextWindow != 200000 {
		t.Fatalf("MaxContextWindow = %d, want %d", info.MaxContextWindow, 200000)
	}
}

func TestGetModelInfoAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "unknown-model", server.URL, AuthModeAPIKey)
	_, err := client.GetModelInfo(context.Background())
	if err == nil {
		t.Fatal("GetModelInfo() should return error for 404")
	}
}

func TestGetModelInfoNetworkError(t *testing.T) {
	// Server that's already closed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := NewClient("test-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)
	_, err := client.GetModelInfo(context.Background())
	if err == nil {
		t.Fatal("GetModelInfo() should return error when server is unreachable")
	}
}

func TestListModelsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Write([]byte(`{
			"data": [
				{"id": "claude-sonnet-4-6", "display_name": "Claude Sonnet 4.6", "max_context_window": 200000},
				{"id": "claude-opus-4-7", "display_name": "Claude Opus 4.7", "max_context_window": 200000}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "claude-sonnet-4-6" {
		t.Fatalf("models[0].ID = %q, want %q", models[0].ID, "claude-sonnet-4-6")
	}
	if models[0].MaxContextWindow != 200000 {
		t.Fatalf("models[0].MaxContextWindow = %d, want 200000", models[0].MaxContextWindow)
	}
	if models[1].DisplayName != "Claude Opus 4.7" {
		t.Fatalf("models[1].DisplayName = %q, want %q", models[1].DisplayName, "Claude Opus 4.7")
	}
}

func TestListModelsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	client := NewClient("bad-key", "claude-sonnet-4-6", server.URL, AuthModeAPIKey)
	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels() should return error for 401")
	}
}
