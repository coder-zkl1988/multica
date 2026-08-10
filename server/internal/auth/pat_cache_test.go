package auth

import (
	"context"
	"testing"
	"time"
)

func TestPATCache_NilSafe(t *testing.T) {
	var cache *PATCache
	ctx := context.Background()
	if value, ok := cache.Get(ctx, "hash"); ok || value != "" {
		t.Fatalf("nil cache must miss; got (%q, %v)", value, ok)
	}
	cache.Set(ctx, "any-hash", "user-1", AuthCacheTTL) // no panic
	cache.Invalidate(ctx, "any-hash")                  // no panic
}

func TestNewPATCache_NilRedisReturnsNil(t *testing.T) {
	if cache := NewPATCache(nil); cache != nil {
		t.Fatalf("NewPATCache(nil) = %#v", cache)
	}
}

func TestPATCache_SetGetInvalidate(t *testing.T) {
	cache := NewPATCache(newRedisTestClient(t))
	ctx := context.Background()
	if _, ok := cache.Get(ctx, "missing"); ok {
		t.Fatal("expected cache miss")
	}
	cache.Set(ctx, "hash", "user", AuthCacheTTL)
	if value, ok := cache.Get(ctx, "hash"); !ok || value != "user" {
		t.Fatalf("cache hit = (%q, %v)", value, ok)
	}
	cache.Invalidate(ctx, "hash")
	if _, ok := cache.Get(ctx, "hash"); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestPATCache_SetRespectsTTL(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewPATCache(rdb)
	ctx := context.Background()
	cache.Set(ctx, "short", "user", 5*time.Second)
	ttl, err := rdb.TTL(ctx, patCacheKey("short")).Result()
	if err != nil || ttl <= 0 || ttl > 6*time.Second {
		t.Fatalf("TTL = %v, err %v", ttl, err)
	}
	cache.Set(ctx, "zero", "user", 0)
	if _, ok := cache.Get(ctx, "zero"); ok {
		t.Fatal("zero TTL must not cache")
	}
}
