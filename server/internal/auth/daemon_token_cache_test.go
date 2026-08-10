package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newRedisTestClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

func TestDaemonTokenCache_NilSafe(t *testing.T) {
	var c *DaemonTokenCache // nil
	ctx := context.Background()

	if id, ok := c.Get(ctx, "any-hash"); ok || id != (DaemonTokenIdentity{}) {
		t.Fatalf("nil cache must miss; got (%+v, %v)", id, ok)
	}
	c.Set(ctx, "any-hash", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, AuthCacheTTL)
	c.Invalidate(ctx, "any-hash")
}

func TestNewDaemonTokenCache_NilRedisReturnsNil(t *testing.T) {
	if c := NewDaemonTokenCache(nil); c != nil {
		t.Fatalf("NewDaemonTokenCache(nil) must return nil, got %#v", c)
	}
}

func TestDaemonTokenCache_SetGetInvalidate(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	if _, ok := c.Get(ctx, "missing"); ok {
		t.Fatal("expected miss before set")
	}

	want := DaemonTokenIdentity{WorkspaceID: "ws-uuid", DaemonID: "daemon-1"}
	c.Set(ctx, "hash-D", want, AuthCacheTTL)
	if got, ok := c.Get(ctx, "hash-D"); !ok || got != want {
		t.Fatalf("expected hit %+v, got (%+v, %v)", want, got, ok)
	}

	c.Invalidate(ctx, "hash-D")
	if _, ok := c.Get(ctx, "hash-D"); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestDaemonTokenCache_TTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-T", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, AuthCacheTTL)
	ttl, err := rdb.TTL(ctx, daemonTokenCacheKey("hash-T")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > AuthCacheTTL+time.Second {
		t.Fatalf("unexpected TTL %v (want ~%v)", ttl, AuthCacheTTL)
	}
}

func TestTTLForExpiry(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		expiresAt time.Time
		want      time.Duration
	}{
		"no expiry":  {want: AuthCacheTTL},
		"far future": {expiresAt: now.Add(24 * time.Hour), want: AuthCacheTTL},
		"soon":       {expiresAt: now.Add(10 * time.Second), want: 10 * time.Second},
		"now":        {expiresAt: now, want: 0},
		"past":       {expiresAt: now.Add(-time.Second), want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := TTLForExpiry(now, tc.expiresAt); got != tc.want {
				t.Fatalf("TTLForExpiry() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDaemonTokenCache_Set_RespectsClampedTTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	c := NewDaemonTokenCache(rdb)
	if c == nil {
		t.Fatal("NewDaemonTokenCache returned nil")
	}
	ctx := context.Background()

	c.Set(ctx, "hash-short", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, 5*time.Second)
	ttl, err := rdb.TTL(ctx, daemonTokenCacheKey("hash-short")).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 5*time.Second+time.Second {
		t.Fatalf("expected clamped TTL ~5s, got %v", ttl)
	}

	c.Set(ctx, "hash-zero", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, 0)
	if _, ok := c.Get(ctx, "hash-zero"); ok {
		t.Fatal("zero-TTL Set must not cache")
	}
	c.Set(ctx, "hash-neg", DaemonTokenIdentity{WorkspaceID: "w", DaemonID: "d"}, -time.Second)
	if _, ok := c.Get(ctx, "hash-neg"); ok {
		t.Fatal("negative-TTL Set must not cache")
	}
}
