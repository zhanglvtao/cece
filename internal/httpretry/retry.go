package httpretry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"
)

const (
	defaultMaxAttempts    = 3
	defaultBaseDelay      = 1 * time.Second
	defaultMaxDelay       = 10 * time.Second
	defaultAuthRetries    = 2
	defaultAuthRetryDelay = 1 * time.Second
)

// retryableStatusCodes are HTTP status codes that indicate a transient error
// worth retrying.
var retryableStatusCodes = map[int]bool{
	429: true, // rate limit
	502: true, // bad gateway
	503: true, // service unavailable
	504: true, // gateway timeout
}

// ShouldRetry determines whether an HTTP response should be retried.
// Return true to retry, false to give up.
type ShouldRetry func(resp *http.Response, err error) bool

// RetryableStatusCodes returns true for transient HTTP status codes (429, 502, 503, 504).
func RetryableStatusCodes(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return retryableStatusCodes[resp.StatusCode]
}

// Options configures retry behavior.
type Options struct {
	MaxAttempts int           // total attempts (including first). 0 = default (3)
	BaseDelay   time.Duration // initial backoff delay. 0 = default (1s)
	MaxDelay    time.Duration // cap on backoff delay. 0 = default (10s)
	ShouldRetry ShouldRetry   // custom retry predicate. nil = RetryableStatusCodes
}

// Do executes an HTTP request with retry logic for transient errors.
// The caller provides a makeRequest function that returns a fresh request each time
// (body is automatically rewound via Seek if the body implements io.Seeker).
func Do(ctx context.Context, httpClient *http.Client, makeRequest func() (*http.Request, error), opts Options) (*http.Response, error) {
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	baseDelay := opts.BaseDelay
	if baseDelay <= 0 {
		baseDelay = defaultBaseDelay
	}
	maxDelay := opts.MaxDelay
	if maxDelay <= 0 {
		maxDelay = defaultMaxDelay
	}
	shouldRetry := opts.ShouldRetry
	if shouldRetry == nil {
		shouldRetry = RetryableStatusCodes
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := makeRequest()
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		// Rewind body if possible (bytes.Reader supports Seek)
		if req.Body != nil {
			if seeker, ok := req.Body.(io.Seeker); ok {
				seeker.Seek(0, io.SeekStart)
			}
		}

		resp, err := httpClient.Do(req)

		if !shouldRetry(resp, err) {
			if err != nil {
				return nil, err
			}
			return resp, nil
		}

		// Drain and close body to reuse connection
		if resp != nil && resp.Body != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		lastErr = err
		if lastErr == nil {
			lastErr = fmt.Errorf("server returned %s", resp.Status)
		}

		if attempt < maxAttempts {
			delay := time.Duration(float64(baseDelay) * math.Pow(2, float64(attempt-1)))
			if delay > maxDelay {
				delay = maxDelay
			}
			slog.Warn("http request failed, retrying",
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"delay", delay,
				"error", lastErr,
			)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
}

// AuthRefreshOptions configures 401 auth-refresh retry behavior.
type AuthRefreshOptions struct {
	MaxAuthRetries int           // max number of 401 retries (after initial request). 0 = default (2)
	AuthRetryDelay time.Duration // delay between 401 retries. 0 = default (1s)
	Invalidate     func()        // called to clear cached credentials before retrying. nil = no 401 retry
}

// DoWithAuthRefresh wraps Do with 401 auth-refresh retry logic.
// On a 401 response, if Invalidate is non-nil, it calls Invalidate to clear cached
// credentials, then retries the request (which should resolve fresh credentials
// via makeRequest). This retries up to MaxAuthRetries times for 401 specifically,
// independent of the transient-error retry in Do.
func DoWithAuthRefresh(ctx context.Context, httpClient *http.Client, makeRequest func() (*http.Request, error), opts Options, authOpts AuthRefreshOptions) (*http.Response, error) {
	maxAuthRetries := authOpts.MaxAuthRetries
	if maxAuthRetries <= 0 {
		maxAuthRetries = defaultAuthRetries
	}
	authRetryDelay := authOpts.AuthRetryDelay
	if authRetryDelay <= 0 {
		authRetryDelay = defaultAuthRetryDelay
	}

	for authAttempt := 0; authAttempt <= maxAuthRetries; authAttempt++ {
		resp, err := Do(ctx, httpClient, makeRequest, opts)
		if err != nil {
			return nil, err
		}

		// Not 401 — return as-is (could be 200 or another error)
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}

		// 401 but no Invalidate callback — cannot refresh, return the 401 response
		if authOpts.Invalidate == nil {
			return resp, nil
		}

		// Drain and close body before retrying
		if resp.Body != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		// Last auth attempt exhausted — return error
		if authAttempt >= maxAuthRetries {
			return nil, fmt.Errorf("server returned 401 Unauthorized after %d auth-refresh attempts", maxAuthRetries+1)
		}

		slog.Warn("401 Unauthorized, invalidating credentials and retrying",
			"auth_attempt", authAttempt+1,
			"max_auth_retries", maxAuthRetries,
		)
		authOpts.Invalidate()

		select {
		case <-time.After(authRetryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("server returned 401 Unauthorized after auth-refresh attempts")
}
