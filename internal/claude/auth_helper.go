package claude

import (
	"github.com/zhanglvtao/cece/internal/auth"
	"time"
)

// TokenCache is re-exported from the shared auth package for backward compatibility.
type TokenCache = auth.TokenCache

// NewTokenCache delegates to auth.NewTokenCache.
func NewTokenCache(helper string, ttl time.Duration) *TokenCache {
	return auth.NewTokenCache(helper, ttl)
}
