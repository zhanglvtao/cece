package auth

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// TokenCache caches a token obtained from an external auth helper command.
type TokenCache struct {
	helper    string
	token     string
	expiresAt time.Time
	mu        sync.Mutex
	ttl       time.Duration
}

// NewTokenCache creates a cache that fetches tokens by executing helper.
// Tokens are considered stale after ttl (default 30 minutes if zero).
func NewTokenCache(helper string, ttl time.Duration) *TokenCache {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &TokenCache{helper: helper, ttl: ttl}
}

// GetToken returns a cached token if still fresh, otherwise executes the
// helper command and caches the result.
func (c *TokenCache) GetToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.expiresAt) {
		return c.token, nil
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		if c.token != "" {
			return c.token, nil
		}
		return "", err
	}

	c.token = token
	c.expiresAt = time.Now().Add(c.ttl)
	return c.token, nil
}

// Invalidate clears the cached token so the next GetToken call fetches a fresh one.
func (c *TokenCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.expiresAt = time.Time{}
}

func (c *TokenCache) fetchToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.helper)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("auth helper exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("auth helper: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
