package inmemory

import "github.com/swasthikshetty10/go-ratelimiter/limiter"

func init() {
	limiter.RegisterInMemory(limiter.AlgorithmFixedWindow, limiter.Spec{
		Validate: limiter.ValidateLimitAndWindow,
		Build: func(c limiter.Config) (limiter.Limiter, error) {
			return NewFixedWindow(c.Limit, c.Window, c.Clock), nil
		},
	})

	limiter.RegisterInMemory(limiter.AlgorithmSlidingWindowCounter, limiter.Spec{
		Validate: limiter.ValidateLimitAndWindow,
		Build: func(c limiter.Config) (limiter.Limiter, error) {
			return NewSlidingWindowCounter(c.Limit, c.Window, c.Clock), nil
		},
	})

	limiter.RegisterInMemory(limiter.AlgorithmTokenBucket, limiter.Spec{
		Validate: limiter.ValidateRateAndCapacity,
		Build: func(c limiter.Config) (limiter.Limiter, error) {
			return NewTokenBucket(c.Rate, c.Capacity, c.Clock), nil
		},
	})

	limiter.RegisterInMemory(limiter.AlgorithmLeakyBucket, limiter.Spec{
		Validate: limiter.ValidateRateAndCapacity,
		Build: func(c limiter.Config) (limiter.Limiter, error) {
			return NewLeakyBucket(c.Rate, c.Capacity, c.Clock), nil
		},
	})
}

// New creates an in-memory limiter for the given algorithm.
// Importing this package registers all supported algorithms.
func New(algo limiter.Algorithm, opts ...limiter.Option) (limiter.Limiter, error) {
	return limiter.NewInMemory(algo, opts...)
}
