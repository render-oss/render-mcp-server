package session_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/render-oss/render-mcp-server/pkg/session"
)

// Exercises the in-memory store the way the HTTP transport does: many requests,
// each in its own goroutine, some sharing a session ID. Run under -race to
// catch the concurrent map access and the session-field data race.
func TestInMemoryStoreConcurrentAccess(t *testing.T) {
	store := session.NewInMemoryStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// A small key space forces both new-key writes and shared-session
			// contention.
			id := fmt.Sprintf("session-%d", n%8)
			sess, err := store.Get(ctx, id)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			if err := sess.SetWorkspace(ctx, fmt.Sprintf("ws-%d", n)); err != nil {
				t.Errorf("SetWorkspace: %v", err)
			}
			if _, err := sess.GetWorkspace(ctx); err != nil {
				t.Errorf("GetWorkspace: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
