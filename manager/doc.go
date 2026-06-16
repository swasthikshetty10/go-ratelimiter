// Package manager maps keys (e.g. user IDs) to limiter.Limiter instances
// for in-memory, per-key rate limiting.
//
// Call Get on every request (e.g. in middleware), then Allow on the limiter:
//
//	lim, err := mgr.Get(userID)
//	if err != nil { ... }
//	result := lim.Allow()
//
// Do not cache limiter.Limiter values across requests without calling Get again —
// idle eviction uses last-access time updated by Get.
//
// Optional idle cleanup: set Config.MaxIdle and run Purge periodically or use RunPurge.
//
// Distributed backends (Redis) embed the key in Allow(ctx, key) — use those
// directly without Manager.
package manager
