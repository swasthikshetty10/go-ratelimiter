package limiter

import (
	"sync"
	"time"
)

type SlidingWindowCounter struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	currCount   float64
	prevCount   float64
	windowStart time.Time
	clock       Clock
}

func NewSlidingWindowCounter(limit int, window time.Duration) *SlidingWindowCounter {

	clock := RealClock{}
	return &SlidingWindowCounter{
		limit:       limit,
		window:      window,
		currCount:   0,
		prevCount:   0,
		windowStart: clock.Now(),
		clock:       clock,
	}
}

func (s *SlidingWindowCounter) Allow() Result {
	return s.AllowN(1)
}

func (s *SlidingWindowCounter) AllowN(n int) Result {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	elapsed := now.Sub(s.windowStart)
	if elapsed < 0 {
		elapsed = 0
	}

	if elapsed >= s.window {
		windowsPassed := int(elapsed / s.window)

		if windowsPassed == 1 {
			s.prevCount = s.currCount
		} else {
			s.prevCount = 0
		}

		s.currCount = 0
		s.windowStart = s.windowStart.Add(
			time.Duration(windowsPassed) * s.window,
		)

		elapsed = now.Sub(s.windowStart)
	}

	weight := 1 - float64(elapsed)/float64(s.window)

	estimatedCount := s.currCount + (s.prevCount * weight)

	retryAfter := s.window - elapsed
	if retryAfter < 0 {
		retryAfter = 0
	}

	if n <= 0 {
		return Result{
			Allowed:    true,
			Remaining:  max(0, int(float64(s.limit)-estimatedCount)),
			Limit:      s.limit,
			RetryAfter: 0,
		}
	}

	if estimatedCount+float64(n) > float64(s.limit) {
		return Result{
			Allowed:    false,
			Remaining:  0,
			Limit:      s.limit,
			RetryAfter: retryAfter,
		}
	}

	s.currCount += float64(n)

	return Result{
		Allowed:    true,
		Remaining:  max(0, int(float64(s.limit)-(estimatedCount+float64(n)))),
		Limit:      s.limit,
		RetryAfter: 0,
	}
}
