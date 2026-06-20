package identity

import (
	"context"
	"sync"
	"time"
)

// TokenFunc fetches a fresh token (e.g. Client.ServiceToken / Client.BootstrapToken bound to specific args).
type TokenFunc func(ctx context.Context) (Token, error)

// CachedToken memoizes a workload token and transparently refreshes it shortly
// before expiry. It is safe for concurrent use and replaces the per-call token
// caches that services previously hand-rolled.
type CachedToken struct {
	refresh TokenFunc

	mu  sync.Mutex
	tok Token
}

// NewCachedToken wraps a TokenFunc with caching.
func NewCachedToken(refresh TokenFunc) *CachedToken {
	return &CachedToken{refresh: refresh}
}

// refreshSkew is how long before expiry the token is proactively refreshed.
const refreshSkew = time.Minute

// Value returns a valid bearer token string, fetching/refreshing as needed.
func (c *CachedToken) Value(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok.Value != "" && time.Now().Before(c.tok.ExpiresAt.Add(-refreshSkew)) {
		return c.tok.Value, nil
	}
	tok, err := c.refresh(ctx)
	if err != nil {
		return "", err
	}
	c.tok = tok
	return c.tok.Value, nil
}
