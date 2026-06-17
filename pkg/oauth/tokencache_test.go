package oauth

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTokenCache_EvictsExpiredEntriesAtCapacity(t *testing.T) {
	c := newTokenCache(4)
	now := time.Now()
	for k := range 4 {
		c.put(fmt.Sprintf("expired-%d", k), IntrospectionResponse{}, now.Add(-time.Second), now)
	}

	c.put("live", IntrospectionResponse{Active: true}, now.Add(time.Minute), now)

	resp, ok := c.get("live", now)
	require.True(t, ok)
	require.True(t, resp.Active)
	require.Len(t, c.entries, 1, "expired entries should have been swept")
}

func TestTokenCache_EvictsNearestExpiryFirst(t *testing.T) {
	c := newTokenCache(4)
	now := time.Now()
	// Fill to capacity with unexpired entries, soonest-to-expire first.
	for k := range 4 {
		c.put(fmt.Sprintf("k%d", k), IntrospectionResponse{}, now.Add(time.Duration(k+1)*time.Minute), now)
	}

	// A further put evicts down to three-quarters capacity (target 3); the
	// single soonest-to-expire entry ("k0") is the one dropped.
	c.put("k-new", IntrospectionResponse{}, now.Add(time.Hour), now)

	_, ok := c.get("k0", now)
	require.False(t, ok, "soonest-to-expire entry should be evicted first")
	for _, k := range []string{"k1", "k2", "k3", "k-new"} {
		_, ok := c.get(k, now)
		require.True(t, ok, "later-expiring entry %q should survive", k)
	}
}

func TestTokenCache_BoundedWhenNothingIsExpired(t *testing.T) {
	c := newTokenCache(4)
	now := time.Now()
	for k := range 10 {
		c.put(fmt.Sprintf("live-%d", k), IntrospectionResponse{}, now.Add(time.Minute), now)
	}

	require.LessOrEqual(t, len(c.entries), 4)
}

func TestTokenCache_ExpiredEntryDeletedOnLookup(t *testing.T) {
	c := newTokenCache(4)
	now := time.Now()
	c.put("token", IntrospectionResponse{}, now.Add(time.Second), now)

	_, ok := c.get("token", now.Add(2*time.Second))

	require.False(t, ok)
	require.Empty(t, c.entries)
}
