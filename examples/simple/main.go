// Simple: one limiter for the whole service (no manager, no per-user keys).
//
// Run: go run ./examples/simple/
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
)

func main() {
	lim, err := inmemory.New(
		limiter.AlgorithmSlidingWindowCounter,
		limiter.WithLimit(3),
		limiter.WithWindow(time.Minute),
	)
	if err != nil {
		log.Fatal(err)
	}

	for i := 1; i <= 4; i++ {
		r := lim.Allow()
		fmt.Printf("request %d: allowed=%v remaining=%d\n", i, r.Allowed, r.Remaining)
	}
}
