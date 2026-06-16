// Package limiter provides thread-safe rate limiting with a small core contract
// and an in-memory factory.
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
// Or import inmemory directly (registers algorithms via init, no blank import needed):
//
//	import (
//	    "time"
//
//	    "github.com/swasthikshetty10/go-ratelimiter/limiter"
//	    "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
//	)
//
//	l, err := inmemory.New(limiter.AlgorithmSlidingWindowCounter,
//	    limiter.WithLimit(100), limiter.WithWindow(time.Minute))
//
// For distributed limits, use limiter/redis — a separate keyed API that shares
// only limiter.Result and validators with this package.
package limiter
