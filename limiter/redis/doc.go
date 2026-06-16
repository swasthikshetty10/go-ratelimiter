// Package redis provides distributed rate limiting backed by Redis.
//
// Unlike the in-memory backend, Redis limiters use a keyed API — pass the
// tenant or user id on each admission call. State is stored in Redis, not on
// the Go struct. Use WithKeyPrefix to namespace keys (e.g. "ratelimit:").
//
// Example:
//
//	bucket, err := redis.NewTokenBucket(
//	    redis.WithClient(rdb),
//	    redis.WithKeyPrefix("ratelimit:"),
//	    redis.WithRate(10),
//	    redis.WithCapacity(100),
//	)
//	r := bucket.AllowKey(ctx, "user:"+userID)
//
// This package does not implement limiter.Limiter and is not registered with
// the core limiter factory. Shared types: limiter.Result and validators.
//
// Redis failures fail closed (deny) — see docs/ARCHITECTURE.md.
// Redis-protocol servers work via go-redis's redis.Scripter.
package redis
