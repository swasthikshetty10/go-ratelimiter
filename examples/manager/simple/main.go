// Simple manager: same limit for every user. NewLimiter ignores the key.
//
// Run: go run ./examples/manager/simple/
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
	"github.com/swasthikshetty10/go-ratelimiter/manager"
)

func main() {
	mgr, err := manager.New(manager.Config{
		NewLimiter: func(_ string) (limiter.Limiter, error) {
			return inmemory.New(
				limiter.AlgorithmSlidingWindowCounter,
				limiter.WithLimit(2),
				limiter.WithWindow(time.Minute),
			)
		},
		MaxIdle: 30 * time.Minute,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, userID := range []string{"alice", "alice", "alice", "bob"} {
		lim, err := mgr.Get(userID)
		if err != nil {
			log.Fatal(err)
		}
		r := lim.Allow()
		fmt.Printf("user=%s allowed=%v remaining=%d\n", userID, r.Allowed, r.Remaining)
	}
}
