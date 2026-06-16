// Package limiter provides thread-safe rate limiting with a small core contract
// and pluggable in-memory (and future distributed) backends.
//
// Create a limiter via the factory:
//
//	import (
//	    "time"
//
//	    "github.com/swasthikshetty10/go-ratelimiter/limiter"
//	    _ "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
//	)
//
//	l, err := limiter.NewInMemory(
//	    limiter.AlgorithmSlidingWindowCounter,
//	    limiter.WithLimit(100),
//	    limiter.WithWindow(time.Minute),
//	)
//
// Or use the inmemory package alias:
//
//	l, err := inmemory.New(limiter.AlgorithmSlidingWindowCounter,
//	    limiter.WithLimit(100), limiter.WithWindow(time.Minute))
package limiter
