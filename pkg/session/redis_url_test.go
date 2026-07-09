package session

import (
	"testing"
)

func TestNewRedisStoreParsesURL(t *testing.T) {
	t.Run("redis URL carries addr and credentials without TLS", func(t *testing.T) {
		store, err := NewRedisStore("redis://user:pass@example.com:6380")
		if err != nil {
			t.Fatalf("NewRedisStore: %v", err)
		}
		opts := store.(*redisStore).c.Options()
		if opts.Addr != "example.com:6380" {
			t.Errorf("Addr = %q, want %q", opts.Addr, "example.com:6380")
		}
		if opts.Username != "user" || opts.Password != "pass" {
			t.Errorf("credentials = %q/%q, want user/pass", opts.Username, opts.Password)
		}
		if opts.TLSConfig != nil {
			t.Error("TLSConfig should be nil for redis://")
		}
	})

	t.Run("rediss URL enables TLS", func(t *testing.T) {
		store, err := NewRedisStore("rediss://user:pass@example.com:6380")
		if err != nil {
			t.Fatalf("NewRedisStore: %v", err)
		}
		opts := store.(*redisStore).c.Options()
		if opts.TLSConfig == nil {
			t.Fatal("TLSConfig should be set for rediss://")
		}
		if opts.TLSConfig.ServerName != "example.com" {
			t.Errorf("ServerName = %q, want %q", opts.TLSConfig.ServerName, "example.com")
		}
	})

	t.Run("non-redis scheme is rejected", func(t *testing.T) {
		if _, err := NewRedisStore("http://example.com:6380"); err == nil {
			t.Fatal("expected error for http:// URL")
		}
	})
}
