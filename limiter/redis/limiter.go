package redis

import (
	"context"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

// KeyLimiter is the Redis-native admission API. Keys are passed per call;
// state lives in Redis, not on the Go struct.
type KeyLimiter interface {
	AllowKey(ctx context.Context, key string) limiter.Result
	AllowNKey(ctx context.Context, key string, n int) limiter.Result
}
