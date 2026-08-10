package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const patCachePrefix = "mul:auth:pat:"

type PATCache struct {
	rdb *redis.Client
}

func NewPATCache(rdb *redis.Client) *PATCache {
	if rdb == nil {
		return nil
	}
	return &PATCache{rdb: rdb}
}

func patCacheKey(hash string) string { return patCachePrefix + hash }

func (c *PATCache) Get(ctx context.Context, hash string) (string, bool) {
	if c == nil {
		return "", false
	}
	value, err := c.rdb.Get(ctx, patCacheKey(hash)).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("pat_cache: get failed; falling back to DB", "error", err)
		}
		return "", false
	}
	return value, true
}

func (c *PATCache) Set(ctx context.Context, hash, userID string, ttl time.Duration) {
	if c == nil || ttl <= 0 {
		return
	}
	if err := c.rdb.Set(ctx, patCacheKey(hash), userID, ttl).Err(); err != nil {
		slog.Warn("pat_cache: set failed", "error", err)
	}
}

func (c *PATCache) Invalidate(ctx context.Context, hash string) {
	if c == nil {
		return
	}
	if err := c.rdb.Del(ctx, patCacheKey(hash)).Err(); err != nil {
		slog.Warn("pat_cache: invalidate failed; entry will expire on TTL", "error", err)
	}
}
