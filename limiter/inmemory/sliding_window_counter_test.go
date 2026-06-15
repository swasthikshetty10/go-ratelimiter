package inmemory

import (
	"sync"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

func newTestSlidingWindowCounter(t *testing.T, limit int, window time.Duration, start time.Time) *SlidingWindowCounter {
	t.Helper()
	sw := NewSlidingWindowCounter(limit, window)
	sw.clock = &fakeClock{now: start}
	sw.windowStart = start
	return sw
}

func TestSlidingWindowCounter_ImplementsLimiter(t *testing.T) {
	var _ limiter.Limiter = (*SlidingWindowCounter)(nil)
}

func TestSlidingWindowCounter_Allow_withinLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 5, time.Minute, start)

	for i := range 5 {
		got := sw.Allow()
		if !got.Allowed {
			t.Fatalf("Allow() #%d: expected allowed", i+1)
		}
		if got.Limit != 5 {
			t.Fatalf("Limit = %d, want 5", got.Limit)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("RetryAfter = %v, want 0 when allowed", got.RetryAfter)
		}
	}
}

func TestSlidingWindowCounter_Allow_deniesAtLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 3, 10*time.Second, start)

	for range 3 {
		if !sw.Allow().Allowed {
			t.Fatal("expected allowed within limit")
		}
	}

	got := sw.Allow()
	if got.Allowed {
		t.Fatal("expected denied at limit")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}
	if got.RetryAfter != 10*time.Second {
		t.Fatalf("RetryAfter = %v, want 10s", got.RetryAfter)
	}
}

func TestSlidingWindowCounter_weightDecay(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(10, time.Minute)
	sw.clock = clock
	sw.windowStart = start

	for range 10 {
		if !sw.Allow().Allowed {
			t.Fatal("expected to fill first window")
		}
	}
	if sw.Allow().Allowed {
		t.Fatal("expected denied at limit in first window")
	}

	// One full window later: prev=10, weight=1 at boundary → still at limit.
	clock.Advance(time.Minute)
	if sw.Allow().Allowed {
		t.Fatal("expected denied at new window start while prev weight is 1")
	}

	// Halfway into new window: estimated = 10 * 0.5 = 5 → five more allowed.
	clock.Advance(30 * time.Second)

	allowed := 0
	for range 6 {
		if sw.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("after 50%% decay: %d allowed, want 5", allowed)
	}
}

func TestSlidingWindowCounter_smoothingVsFixedWindowBoundary(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}

	sw := NewSlidingWindowCounter(10, time.Minute)
	sw.clock = clock
	sw.windowStart = start

	fw := NewFixedWindow(10, time.Minute)
	fw.clock = clock
	fw.windowStart = start

	for range 10 {
		sw.Allow()
		fw.Allow()
	}

	clock.Advance(time.Minute)

	// Fixed window resets fully — immediate burst of 10.
	fwAllowed := 0
	for range 11 {
		if fw.Allow().Allowed {
			fwAllowed++
		}
	}
	if fwAllowed != 10 {
		t.Fatalf("fixed window allowed %d after reset, want 10", fwAllowed)
	}

	// Sliding window: prev window still weighted at boundary → 0 immediate allows.
	swAllowed := 0
	for range 11 {
		if sw.Allow().Allowed {
			swAllowed++
		}
	}
	if swAllowed != 0 {
		t.Fatalf("sliding counter allowed %d at boundary, want 0 (smoothing)", swAllowed)
	}
}

func TestSlidingWindowCounter_idleBeyondTwoWindows(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(5, time.Second)
	sw.clock = clock
	sw.windowStart = start

	for range 5 {
		sw.Allow()
	}

	// Jump past two windows — prev and curr both cleared.
	clock.Advance(3 * time.Second)

	allowed := 0
	for range 6 {
		if sw.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("after long idle: %d allowed, want 5 (full reset)", allowed)
	}
}

func TestSlidingWindowCounter_singleWindowRollover(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(10, time.Minute)
	sw.clock = clock
	sw.windowStart = start

	for range 4 {
		sw.Allow()
	}

	clock.Advance(time.Minute)

	// prev=4, weight=1 → estimated 4, 6 slots left.
	allowed := 0
	for range 7 {
		if sw.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 6 {
		t.Fatalf("after one window rollover: %d allowed, want 6", allowed)
	}
}

func TestSlidingWindowCounter_exactWindowBoundary(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(5, time.Second)
	sw.clock = clock
	sw.windowStart = start

	sw.Allow()
	sw.Allow()

	// Exactly one window elapsed — rollover with prev=2, weight=1 → estimated=2, 3 slots left.
	clock.Advance(time.Second)

	got := sw.Allow()
	if !got.Allowed {
		t.Fatal("expected allowed after rollover with partial prev window")
	}
	if got.Remaining != 2 {
		t.Fatalf("Remaining = %d, want 2 after one admit", got.Remaining)
	}
}

func TestSlidingWindowCounter_AllowN_success(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 10, time.Second, start)

	got := sw.AllowN(4)
	if !got.Allowed {
		t.Fatal("AllowN(4): expected allowed")
	}
	if got.Remaining != 6 {
		t.Fatalf("Remaining = %d, want 6", got.Remaining)
	}

	got = sw.AllowN(6)
	if !got.Allowed {
		t.Fatal("AllowN(6): expected allowed")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestSlidingWindowCounter_AllowN_rejectsWithoutPartialAdmit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 8, time.Second, start)

	sw.AllowN(6)

	got := sw.AllowN(3)
	if got.Allowed {
		t.Fatal("AllowN(3) with ~2 slots: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}

	// Count unchanged — two singles should still succeed.
	if !sw.Allow().Allowed {
		t.Fatal("expected first remaining slot")
	}
	if !sw.Allow().Allowed {
		t.Fatal("expected second remaining slot")
	}
	if sw.Allow().Allowed {
		t.Fatal("expected denied after exhausting limit")
	}
}

func TestSlidingWindowCounter_AllowN_exceedsLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 5, time.Second, start)

	got := sw.AllowN(6)
	if got.Allowed {
		t.Fatal("AllowN(6) with limit 5: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestSlidingWindowCounter_AllowN_zeroAndNegative(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 5, time.Second, start)

	sw.AllowN(3)

	for _, n := range []int{0, -1} {
		got := sw.AllowN(n)
		if !got.Allowed {
			t.Fatalf("AllowN(%d): expected allowed (no-op)", n)
		}
		if got.Remaining != 2 {
			t.Fatalf("AllowN(%d): Remaining = %d, want 2", n, got.Remaining)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("AllowN(%d): RetryAfter = %v, want 0", n, got.RetryAfter)
		}
	}
}

func TestSlidingWindowCounter_RetryAfter_decreasesOverWindow(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(1, 10*time.Second)
	sw.clock = clock
	sw.windowStart = start

	sw.Allow()
	got := sw.Allow()
	if got.RetryAfter != 10*time.Second {
		t.Fatalf("RetryAfter = %v, want 10s", got.RetryAfter)
	}

	clock.Advance(4 * time.Second)
	got = sw.Allow()
	if got.RetryAfter != 6*time.Second {
		t.Fatalf("RetryAfter = %v, want 6s", got.RetryAfter)
	}
}

func TestSlidingWindowCounter_clockBackward(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	sw := NewSlidingWindowCounter(2, time.Second)
	sw.clock = clock
	sw.windowStart = start

	sw.Allow()
	sw.Allow()
	if sw.Allow().Allowed {
		t.Fatal("expected denied at limit")
	}

	clock.now = start.Add(-time.Second)

	got := sw.Allow()
	if got.Allowed {
		t.Fatal("expected still denied after backward clock jump")
	}
}

func TestSlidingWindowCounter_remainingIsAdvisory(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sw := newTestSlidingWindowCounter(t, 10, time.Minute, start)

	for i := range 9 {
		got := sw.Allow()
		if !got.Allowed {
			t.Fatalf("request %d: expected allowed", i+1)
		}
		if got.Remaining < 0 {
			t.Fatalf("Remaining = %d, must not be negative", got.Remaining)
		}
	}
}

func TestSlidingWindowCounter_Concurrent(t *testing.T) {
	sw := NewSlidingWindowCounter(1000, time.Minute)

	var wg sync.WaitGroup
	const goroutines = 50
	const perG = 20

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perG {
				sw.Allow()
			}
		}()
	}
	wg.Wait()

	denied := 0
	for range 100 {
		if !sw.Allow().Allowed {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("expected some requests denied after concurrent burst")
	}
}

func TestSlidingWindowCounter_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race stress test in short mode")
	}
	sw := NewSlidingWindowCounter(100, time.Minute)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sw.Allow()
			sw.AllowN(2)
		}()
	}
	wg.Wait()
}
