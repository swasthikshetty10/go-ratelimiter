package inmemory

import (
	"sync"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

type TokenBucket struct {
	mu         sync.Mutex
	rate       float64
	capacity   int
	tokens     float64
	lastRefill time.Time
	clock      limiter.Clock
}

func NewTokenBucket(rate float64, capacity int, clock limiter.Clock) *TokenBucket {
	if clock == nil {
		clock = limiter.RealClock{}
	}
	return &TokenBucket{
		rate:       rate,
		capacity:   capacity,
		tokens:     float64(capacity),
		lastRefill: clock.Now(),
		clock:      clock,
	}
}

func (t *TokenBucket) Allow() limiter.Result {
	return t.AllowN(1)
}

func (t *TokenBucket) AllowN(n int) limiter.Result {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.clock.Now()
	elapsed := now.Sub(t.lastRefill)
	if elapsed < 0 {
		elapsed = 0
	}

	tokens := t.tokens + elapsed.Seconds()*t.rate
	t.tokens = min(float64(t.capacity), tokens)
	t.lastRefill = now

	if n <= 0 {
		return limiter.Result{
			Allowed:    true,
			Remaining:  int(t.tokens),
			Limit:      t.capacity,
			RetryAfter: 0,
		}
	}

	if t.tokens < float64(n) {
		missing := float64(n) - t.tokens
		var retryAfter time.Duration
		if t.rate > 0 {
			retryAfter = time.Duration(missing / t.rate * float64(time.Second))
		}
		return limiter.Result{
			Allowed:    false,
			Remaining:  0,
			Limit:      t.capacity,
			RetryAfter: retryAfter,
		}
	}

	t.tokens -= float64(n)
	return limiter.Result{
		Allowed:    true,
		Remaining:  int(t.tokens),
		Limit:      t.capacity,
		RetryAfter: 0,
	}
}
