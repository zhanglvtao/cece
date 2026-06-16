package httpretry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryableStatusCodes(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{201, false},
		{204, false},
		{301, false}, // redirects are not transient
		{400, false}, // permanent client errors — must not retry
		{401, false},
		{403, false},
		{404, false},
		{408, true}, // request timeout — transient
		{422, false},
		{429, true}, // rate limited — transient
		{500, true}, // server errors — transient
		{502, true},
		{503, true},
		{504, true},
	}
	for _, tt := range tests {
		resp := &http.Response{StatusCode: tt.code}
		got := RetryableStatusCodes(resp, nil)
		if got != tt.expected {
			t.Errorf("RetryableStatusCodes(%d) = %v, want %v", tt.code, got, tt.expected)
		}
	}
}

func TestRetryableStatusCodes_ContextErrorsNotRetryable(t *testing.T) {
	if RetryableStatusCodes(nil, context.Canceled) {
		t.Fatal("context canceled should not be retryable")
	}
	if RetryableStatusCodes(nil, context.DeadlineExceeded) {
		t.Fatal("context deadline exceeded should not be retryable")
	}
}

func TestRetryableStatusCodes_NetworkError(t *testing.T) {
	if !RetryableStatusCodes(nil, errors.New("connection refused")) {
		t.Error("network errors should be retryable")
	}
}

func TestDo_SucceedsOnFirstAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	var calls atomic.Int32
	makeReq := func() (*http.Request, error) {
		calls.Add(1)
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	resp, err := Do(context.Background(), http.DefaultClient, makeReq, Options{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", calls.Load())
	}
}

func TestDo_RetriesOn502ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	resp, err := Do(context.Background(), http.DefaultClient, makeReq, Options{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDo_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	_, err := Do(context.Background(), http.DefaultClient, makeReq, Options{BaseDelay: 1 * time.Millisecond})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls.Load() != 5 {
		t.Errorf("expected 5 calls, got %d", calls.Load())
	}
}

func TestDo_DoesNotRetry400(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_request"}`))
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	resp, err := Do(context.Background(), http.DefaultClient, makeReq, Options{BaseDelay: 1 * time.Millisecond})
	if err != nil {
		t.Fatalf("expected response, not error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 call (no retry for 400), got %d", calls.Load())
	}
}

func TestDo_RespectsContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	}

	_, err := Do(ctx, http.DefaultClient, makeReq, Options{MaxAttempts: 3, BaseDelay: 1 * time.Millisecond})
	if err == nil {
		t.Fatal("expected error due to cancelled context")
	}
}

func TestDo_CustomShouldRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(418) // I'm a teapot — not in default retryable set
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	customRetry := func(resp *http.Response, err error) bool {
		return err != nil || resp.StatusCode == 418
	}

	_, err := Do(context.Background(), http.DefaultClient, makeReq, Options{
		MaxAttempts: 2,
		BaseDelay:   1 * time.Millisecond,
		ShouldRetry: customRetry,
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls with custom predicate, got %d", calls.Load())
	}
}

func TestDoWithAuthRefresh_Retries401WithInvalidate(t *testing.T) {
	var calls atomic.Int32
	var invalidated atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	invalidate := func() { invalidated.Add(1) }

	resp, err := DoWithAuthRefresh(context.Background(), http.DefaultClient, makeReq, Options{MaxAttempts: 1, BaseDelay: 1 * time.Millisecond}, AuthRefreshOptions{
		Invalidate:     invalidate,
		MaxAuthRetries: 2,
		AuthRetryDelay: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if calls.Load() != 3 {
		t.Errorf("expected 3 calls (initial + 2 retries), got %d", calls.Load())
	}
	if invalidated.Load() != 2 {
		t.Errorf("expected 2 invalidations, got %d", invalidated.Load())
	}
}

func TestDoWithAuthRefresh_NoRetryWithoutInvalidate(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	resp, err := DoWithAuthRefresh(context.Background(), http.DefaultClient, makeReq, Options{MaxAttempts: 1, BaseDelay: 1 * time.Millisecond}, AuthRefreshOptions{
		Invalidate: nil, // no invalidate → should not retry 401
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if calls.Load() != 1 {
		t.Errorf("expected 1 call without invalidate, got %d", calls.Load())
	}
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 response, got %d", resp.StatusCode)
	}
}

func TestDoWithAuthRefresh_ExhaustsAuthRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
	}))
	defer server.Close()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	}

	_, err := DoWithAuthRefresh(context.Background(), http.DefaultClient, makeReq, Options{MaxAttempts: 1, BaseDelay: 1 * time.Millisecond}, AuthRefreshOptions{
		Invalidate:     func() {},
		MaxAuthRetries: 2,
		AuthRetryDelay: 1 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error after exhausting auth retries")
	}
	// 1 initial + 2 auth retries = 3
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestDoWithAuthRefresh_RespectsContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	makeReq := func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	}

	_, err := DoWithAuthRefresh(ctx, http.DefaultClient, makeReq, Options{MaxAttempts: 1, BaseDelay: 1 * time.Millisecond}, AuthRefreshOptions{
		Invalidate:     func() {},
		MaxAuthRetries: 2,
		AuthRetryDelay: 1 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error due to cancelled context")
	}
}
