package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis-backed CapabilityScanStore.
//
// Same layout and the same atomic claim as the local-skill list store
// (runtime_local_skills_redis_store.go):
//
//	<prefix>:<request_id>          → JSON request, TTL = retention
//	<prefix>:pending:<runtime_id>  → ZSET { member = request_id, score = created_at }
//
// PopPending claims through claimPendingScript so ZREM and the running
// transition happen in one step; two API nodes answering the same heartbeat
// storm cannot both hand the same scan to the daemon.
const (
	capabilityScanKeyPrefix     = "mul:" + runtimePendingRedisHashTag + ":capability_scan:"
	capabilityScanPendingPrefix = "mul:" + runtimePendingRedisHashTag + ":capability_scan:pending:"
	capabilityScanPopMaxRetries = 5
)

func capabilityScanKey(id string) string { return capabilityScanKeyPrefix + id }
func capabilityScanPendingKey(runtimeID string) string {
	return capabilityScanPendingPrefix + runtimeID
}

type RedisCapabilityScanStore struct {
	rdb *redis.Client
}

func NewRedisCapabilityScanStore(rdb *redis.Client) *RedisCapabilityScanStore {
	return &RedisCapabilityScanStore{rdb: rdb}
}

func (s *RedisCapabilityScanStore) Create(ctx context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error) {
	now := time.Now()
	req := &RuntimeCapabilityScanRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    CapabilityScanPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal scan request: %w", err)
	}
	requestKey := capabilityScanKey(req.ID)
	pendingKey := capabilityScanPendingKey(runtimeID)
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, requestKey, data, capabilityScanRetention)
	pipe.ZAdd(ctx, pendingKey, redis.Z{Score: float64(now.UnixNano()), Member: req.ID})
	pipe.Expire(ctx, pendingKey, capabilityScanRetention*2)
	if _, err := pipe.Exec(ctx); err != nil {
		_ = s.rdb.Del(ctx, requestKey).Err()
		_ = s.rdb.ZRem(ctx, pendingKey, req.ID).Err()
		return nil, fmt.Errorf("persist scan request: %w", err)
	}
	return req, nil
}

func (s *RedisCapabilityScanStore) Get(ctx context.Context, id string) (*RuntimeCapabilityScanRequest, error) {
	raw, err := s.rdb.Get(ctx, capabilityScanKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get scan request: %w", err)
	}
	var req RuntimeCapabilityScanRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode scan request: %w", err)
	}
	return &req, nil
}

func (s *RedisCapabilityScanStore) persist(ctx context.Context, req *RuntimeCapabilityScanRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal scan request: %w", err)
	}
	if err := s.rdb.Set(ctx, capabilityScanKey(req.ID), data, capabilityScanRetention).Err(); err != nil {
		return fmt.Errorf("persist scan request: %w", err)
	}
	return nil
}

func (s *RedisCapabilityScanStore) HasPending(ctx context.Context, runtimeID string) (bool, error) {
	cnt, err := s.rdb.ZCard(ctx, capabilityScanPendingKey(runtimeID)).Result()
	if err != nil {
		return false, fmt.Errorf("zcard pending: %w", err)
	}
	return cnt > 0, nil
}

func (s *RedisCapabilityScanStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeCapabilityScanRequest, error) {
	pendingKey := capabilityScanPendingKey(runtimeID)
	for attempt := 0; attempt < capabilityScanPopMaxRetries; attempt++ {
		ids, err := s.rdb.ZRange(ctx, pendingKey, 0, 0).Result()
		if err != nil {
			return nil, fmt.Errorf("zrange pending: %w", err)
		}
		if len(ids) == 0 {
			return nil, nil
		}
		id := ids[0]
		req, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if req == nil || req.Status != CapabilityScanPending {
			// Expired record, or another node already claimed it: unlink
			// and look at the next one.
			s.rdb.ZRem(ctx, pendingKey, id)
			continue
		}
		now := time.Now()
		req.Status = CapabilityScanRunning
		req.UpdatedAt = now
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal scan request: %w", err)
		}
		result, err := claimPendingScript.Run(
			ctx, s.rdb,
			[]string{pendingKey, capabilityScanKey(id)},
			id, data, int(capabilityScanRetention.Seconds()),
		).Int64()
		if err != nil {
			return nil, fmt.Errorf("claim pending: %w", err)
		}
		if result == 0 {
			continue
		}
		return req, nil
	}
	return nil, nil
}

func (s *RedisCapabilityScanStore) Complete(ctx context.Context, id string) error {
	req, err := s.Get(ctx, id)
	if err != nil || req == nil {
		return err
	}
	req.Status = CapabilityScanCompleted
	req.Error = ""
	req.UpdatedAt = time.Now()
	return s.persist(ctx, req)
}

func (s *RedisCapabilityScanStore) Fail(ctx context.Context, id string, errMsg string) error {
	req, err := s.Get(ctx, id)
	if err != nil || req == nil {
		return err
	}
	req.Status = CapabilityScanFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return s.persist(ctx, req)
}
