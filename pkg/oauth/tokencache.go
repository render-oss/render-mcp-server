package oauth

import (
	"slices"
	"sync"
	"time"
)

// tokenCache is a size-bounded cache of introspection results keyed by
// sha256(token) (see hashToken), so raw tokens never sit in cache memory.
// Entries carry their own expiry and are dropped lazily on lookup; when the
// cache is full, evictLocked makes room.
type tokenCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]cacheEntry
}

type cacheEntry struct {
	resp      IntrospectionResponse
	expiresAt time.Time
}

func newTokenCache(max int) *tokenCache {
	return &tokenCache{max: max, entries: map[string]cacheEntry{}}
}

func (c *tokenCache) get(key string, now time.Time) (IntrospectionResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return IntrospectionResponse{}, false
	}
	if !now.Before(e.expiresAt) {
		delete(c.entries, key)
		return IntrospectionResponse{}, false
	}
	return e.resp, true
}

func (c *tokenCache) put(key string, resp IntrospectionResponse, expiresAt, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.evictLocked(now)
	}
	c.entries[key] = cacheEntry{resp: resp, expiresAt: expiresAt}
}

// evictLocked makes room when the cache is full: it drops expired entries,
// then the entries closest to expiry until the cache is at three-quarters
// capacity. Nearest-expiry-first sheds the least useful entries; the quarter
// of headroom keeps eviction rare even under a flood of distinct tokens.
func (c *tokenCache) evictLocked(now time.Time) {
	target := c.max * 3 / 4
	for k, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	drop := len(c.entries) - target
	if drop <= 0 {
		return
	}
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return c.entries[a].expiresAt.Compare(c.entries[b].expiresAt)
	})
	for _, k := range keys[:drop] {
		delete(c.entries, k)
	}
}
