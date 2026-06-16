// Package manager maps keys (e.g. user IDs) to limiter.Limiter instances
// for in-memory, per-key rate limiting.
//
// Usage:
//
//	lim, err := mgr.Get(userID)
//	if err != nil { ... }
//	result := lim.Allow()
//
// Distributed backends (Redis) embed the key in Allow(ctx, key) — use those
// directly without Manager.
package manager
