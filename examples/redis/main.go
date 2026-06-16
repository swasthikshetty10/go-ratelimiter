// Redis: distributed token bucket with a key per tenant.
//
// Run: go run ./examples/redis/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	redlimiter "github.com/swasthikshetty10/go-ratelimiter/limiter/redis"
)

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer rdb.Close()

	bucket, err := redlimiter.NewTokenBucket(
		redlimiter.WithClient(rdb),
		redlimiter.WithKeyPrefix("ratelimit:"),
		redlimiter.WithRate(2),
		redlimiter.WithCapacity(10),
	)
	if err != nil {
		log.Fatal(err)
	}

	userID := "user-123"
	for i := 1; i <= 3; i++ {
		r := bucket.AllowKey(ctx, userID)
		fmt.Printf("request %d: allowed=%v remaining=%d\n", i, r.Allowed, r.Remaining)
	}
}
