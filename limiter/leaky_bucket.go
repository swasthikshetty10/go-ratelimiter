package limiter

import (
	"math"
	"sync"
	"time"
)

type LeakyBucket struct {
	mu       sync.Mutex
	rate     float64
	capacity int

	currVolume float64
	lastLeak   time.Time
	clock      Clock
}

func NewLeakyBucket(rate float64, capacity int) *LeakyBucket {
	clock := RealClock{}

	return &LeakyBucket{
		rate:       rate,
		capacity:   capacity,
		currVolume: 0,
		lastLeak:   clock.Now(),
		clock:      clock,
	}
}

func (l *LeakyBucket) Allow() Result {
	return l.AllowN(1)
}

func (l *LeakyBucket) AllowN(n int) Result {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()

	elapsed := now.Sub(l.lastLeak)
	if elapsed < 0 {
		elapsed = 0
	}
	leaked := elapsed.Seconds() * l.rate

	l.currVolume = math.Max(0, l.currVolume-leaked)
	l.lastLeak = now

	freeCapacity := func() int {
		return max(0, int(float64(l.capacity)-l.currVolume))
	}

	if n <= 0 {
		return Result{
			Allowed:    true,
			Remaining:  freeCapacity(),
			Limit:      l.capacity,
			RetryAfter: 0,
		}
	}

	// Reject if bucket would overflow
	if l.currVolume+float64(n) > float64(l.capacity) {
		excess := l.currVolume + float64(n) - float64(l.capacity)

		var retryAfter time.Duration
		if l.rate > 0 {
			retryAfter = time.Duration(
				excess / l.rate * float64(time.Second),
			)
		}

		return Result{
			Allowed:    false,
			Remaining:  0,
			Limit:      l.capacity,
			RetryAfter: retryAfter,
		}
	}

	l.currVolume += float64(n)

	return Result{
		Allowed:    true,
		Remaining:  freeCapacity(),
		Limit:      l.capacity,
		RetryAfter: 0,
	}
}
