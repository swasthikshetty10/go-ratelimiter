package inmemory

import (
	"sync"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

type FixedWindow struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	count       int
	windowStart time.Time
	clock       limiter.Clock
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	clock := limiter.RealClock{}
	return &FixedWindow{
		limit:       limit,
		window:      window,
		clock:       clock,
		count:       0,
		windowStart: clock.Now(),
	}
}

func (f *FixedWindow) Allow() limiter.Result {
	return f.AllowN(1)
}

func (f *FixedWindow) AllowN(n int) limiter.Result {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.clock.Now()
	if now.Sub(f.windowStart) >= f.window {
		f.count = 0
		f.windowStart = now
	}

	retryAfter := f.window - now.Sub(f.windowStart)
	if retryAfter < 0 {
		retryAfter = 0
	}

	if n <= 0 {
		return limiter.Result{
			Allowed:    true,
			Remaining:  f.limit - f.count,
			Limit:      f.limit,
			RetryAfter: 0,
		}
	}

	if f.count+n > f.limit {
		return limiter.Result{
			Allowed:    false,
			Remaining:  0,
			Limit:      f.limit,
			RetryAfter: retryAfter,
		}
	}

	f.count += n
	return limiter.Result{
		Allowed:    true,
		Remaining:  f.limit - f.count,
		Limit:      f.limit,
		RetryAfter: 0,
	}
}
