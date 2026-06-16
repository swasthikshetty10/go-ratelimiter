package redis

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

//go:embed scripts/token_bucket.lua
var tokenBucketScriptSrc string

// tokenBucketScript is built once at init from compile-time embedded bytes
var tokenBucketScript = redis.NewScript(tokenBucketScriptSrc)

var (
	ErrClientRequired = errors.New("redis: client is required")
	ErrEmptyKey       = errors.New("redis: key must not be empty")
)

// TokenBucket is a distributed token bucket backed by Redis.
type TokenBucket struct {
	client   redis.Scripter
	prefix   string
	rate     float64
	capacity int
	script   *redis.Script
}

var _ KeyLimiter = (*TokenBucket)(nil)

// NewTokenBucket creates a Redis-backed token bucket limiter.
func NewTokenBucket(opts ...Option) (*TokenBucket, error) {
	cfg := &buildConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	if cfg.client == nil {
		return nil, ErrClientRequired
	}
	if err := limiter.ValidateRateAndCapacity(cfg.Config); err != nil {
		return nil, err
	}

	return &TokenBucket{
		client:   cfg.client,
		prefix:   cfg.prefix,
		rate:     cfg.Rate,
		capacity: cfg.Capacity,
		script:   tokenBucketScript,
	}, nil
}

func (t *TokenBucket) redisKey(key string) string {
	return t.prefix + key
}

// AllowKey admits one unit for key.
func (t *TokenBucket) AllowKey(ctx context.Context, key string) limiter.Result {
	return t.AllowNKey(ctx, key, 1)
}

// AllowNKey admits n units for key atomically.
func (t *TokenBucket) AllowNKey(ctx context.Context, key string, n int) limiter.Result {
	if key == "" {
		return limiter.Result{Allowed: false, Limit: t.capacity}
	}

	res, err := t.script.Run(ctx, t.client, []string{t.redisKey(key)},
		t.rate,
		t.capacity,
		n,
	).Int64Slice()
	if err != nil || len(res) < 4 {
		// Fail closed on Redis errors or unexpected script output.
		return limiter.Result{Allowed: false, Limit: t.capacity}
	}
	return limiter.Result{
		Allowed:    res[0] == 1,
		Remaining:  int(res[1]),
		Limit:      int(res[2]),
		RetryAfter: time.Duration(res[3]) * time.Millisecond,
	}
}
