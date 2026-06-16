// Quota tiers: dynamic limits from key (free / basic / pro).
//
// Key format: "plan:userID". Replace lookupPlan with your DB or config service.
//
// Run: go run ./examples/manager/quota/
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
	"github.com/swasthikshetty10/go-ratelimiter/manager"
)

func limitForPlan(plan string) int {
	switch plan {
	case "pro":
		return 1000
	case "basic":
		return 100
	default:
		return 10 // free
	}
}

func newLimiterForKey(key string) (limiter.Limiter, error) {
	plan, _, _ := strings.Cut(key, ":")
	return inmemory.New(
		limiter.AlgorithmSlidingWindowCounter,
		limiter.WithLimit(limitForPlan(plan)),
		limiter.WithWindow(time.Minute),
	)
}

func main() {
	mgr, err := manager.New(manager.Config{
		NewLimiter: newLimiterForKey,
		MaxIdle:    30 * time.Minute,
		MaxKeys:    10_000,
	})
	if err != nil {
		log.Fatal(err)
	}

	keys := []string{"free:user-1", "basic:user-2", "pro:user-3"}
	for _, key := range keys {
		lim, err := mgr.Get(key)
		if err != nil {
			log.Fatal(err)
		}
		r := lim.Allow()
		fmt.Printf("key=%-14s limit=%d remaining=%d\n", key, r.Limit, r.Remaining)
	}

	// Plan is fixed at first Get. After upgrade: mgr.Delete("free:user-1")
}
