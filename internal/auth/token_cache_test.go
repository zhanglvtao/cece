package auth

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenCache_GetToken_fresh(t *testing.T) {
	calls := 0
	helper := "echo hello-token"
	cache := NewTokenCache(helper, 10*time.Minute)

	token, err := cache.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	_ = calls

	if token != "hello-token" {
		t.Fatalf("got %q, want %q", token, "hello-token")
	}
}

func TestTokenCache_GetToken_cached(t *testing.T) {
	cache := NewTokenCache("echo first", 10*time.Minute)

	token1, _ := cache.GetToken(context.Background())
	token2, _ := cache.GetToken(context.Background())

	if token1 != token2 {
		t.Fatalf("expected cached token, got %q then %q", token1, token2)
	}
}

func TestTokenCache_Invalidate(t *testing.T) {
	cache := NewTokenCache("echo first", 10*time.Minute)

	_, _ = cache.GetToken(context.Background())
	cache.Invalidate()
	token2, _ := cache.GetToken(context.Background())

	// Same helper returns same value, but Invalidate should cause a re-fetch.
	// We just verify Invalidate doesn't panic and GetToken works after it.
	if token2 != "first" {
		t.Fatalf("got %q after invalidate, want %q", token2, "first")
	}
}

func TestTokenCache_Invalidate_forces_refetch(t *testing.T) {
	var mu sync.Mutex
	var callCount int

	cache := NewTokenCache("", 10*time.Minute)
	// Override helper after creation
	cache.helper = "echo token-v1"

	token1, _ := cache.GetToken(context.Background())
	if token1 != "token-v1" {
		t.Fatalf("got %q, want %q", token1, "token-v1")
	}

	// Change helper output and invalidate
	cache.helper = "echo token-v2"
	cache.Invalidate()

	token2, _ := cache.GetToken(context.Background())
	if token2 != "token-v2" {
		t.Fatalf("got %q after invalidate+helper change, want %q", token2, "token-v2")
	}

	mu.Lock()
	_ = callCount
	mu.Unlock()
}
