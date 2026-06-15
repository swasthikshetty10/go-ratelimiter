package limiter

import (
	"math"
	"sync"
	"testing"
	"time"
)

func newTestTokenBucket(t *testing.T, rate float64, capacity int, start time.Time) *TokenBucket {
	t.Helper()
	tb := NewTokenBucket(rate, capacity)
	tb.clock = &fakeClock{now: start}
	tb.lastRefill = start
	return tb
}

func drainBucket(tb *TokenBucket, clock *fakeClock) {
	for tb.Allow().Allowed {
	}
	clock.Advance(time.Nanosecond) // advance so lastRefill moves past drain instant
}

func TestTokenBucket_ImplementsLimiter(t *testing.T) {
	var _ Limiter = (*TokenBucket)(nil)
}

func TestTokenBucket_startsFull(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 10, 5, start)

	got := tb.AllowN(0)
	if got.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5 (starts at capacity)", got.Remaining)
	}
	if got.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", got.Limit)
	}
}

func TestTokenBucket_initialBurst(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 5, start)

	for i := range 5 {
		got := tb.Allow()
		if !got.Allowed {
			t.Fatalf("Allow() #%d: expected allowed (initial burst)", i+1)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("RetryAfter = %v, want 0 when allowed", got.RetryAfter)
		}
	}

	got := tb.Allow()
	if got.Allowed {
		t.Fatal("6th Allow(): expected denied after burst exhausted")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}
}

func TestTokenBucket_refillOverTime(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(10, 10) // 10 tokens/sec, capacity 10
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)

	got := tb.Allow()
	if got.Allowed {
		t.Fatal("expected denied immediately after drain")
	}
	wantRetry := 100 * time.Millisecond
	if math.Abs(float64(got.RetryAfter-wantRetry)) > float64(time.Millisecond) {
		t.Fatalf("RetryAfter = %v, want ~100ms for 1 token at 10/s", got.RetryAfter)
	}

	clock.Advance(100 * time.Millisecond)

	got = tb.Allow()
	if !got.Allowed {
		t.Fatal("expected allowed after 100ms refill")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 after using last token", got.Remaining)
	}
}

func TestTokenBucket_refillCappedAtCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(1, 5) // 1 token/sec, capacity 5
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)
	clock.Advance(100 * time.Second)

	allowed := 0
	for range 10 {
		if tb.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("after long idle: %d allowed, want 5 (capped at capacity)", allowed)
	}
}

func TestTokenBucket_remainingAfterPartialUse(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 10, start)

	got := tb.AllowN(3)
	if !got.Allowed {
		t.Fatal("AllowN(3): expected allowed")
	}
	if got.Remaining != 7 {
		t.Fatalf("Remaining = %d, want 7", got.Remaining)
	}

	got = tb.AllowN(7)
	if !got.Allowed {
		t.Fatal("AllowN(7): expected allowed")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestTokenBucket_AllowN_rejectsWithoutPartialAdmit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 5, start)

	tb.AllowN(4)

	got := tb.AllowN(2)
	if got.Allowed {
		t.Fatal("AllowN(2) with 1 token left: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}

	got = tb.Allow()
	if !got.Allowed {
		t.Fatal("expected one remaining token still available")
	}
}

func TestTokenBucket_AllowN_exceedsCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 5, start)

	got := tb.AllowN(6)
	if got.Allowed {
		t.Fatal("AllowN(6) with capacity 5: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestTokenBucket_AllowN_zeroAndNegative(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 5, start)

	tb.AllowN(2)

	for _, n := range []int{0, -1} {
		got := tb.AllowN(n)
		if !got.Allowed {
			t.Fatalf("AllowN(%d): expected allowed (no-op)", n)
		}
		if got.Remaining != 3 {
			t.Fatalf("AllowN(%d): Remaining = %d, want 3", n, got.Remaining)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("AllowN(%d): RetryAfter = %v, want 0", n, got.RetryAfter)
		}
	}
}

func TestTokenBucket_retryAfterScalesWithMissingTokens(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(2, 10) // 2 tokens/sec
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)

	got := tb.AllowN(4)
	if got.Allowed {
		t.Fatal("expected denied with 0 tokens")
	}
	want := 2 * time.Second
	if math.Abs(float64(got.RetryAfter-want)) > float64(time.Millisecond) {
		t.Fatalf("RetryAfter = %v, want ~2s", got.RetryAfter)
	}
}

func TestTokenBucket_fractionalRefill(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(10, 10)
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)

	// 50ms → 0.5 tokens — not enough for Allow()
	clock.Advance(50 * time.Millisecond)
	if tb.Allow().Allowed {
		t.Fatal("expected denied with 0.5 tokens")
	}

	// 100ms total → 1 token
	clock.Advance(50 * time.Millisecond)
	if !tb.Allow().Allowed {
		t.Fatal("expected allowed with 1 token after 100ms")
	}
}

func TestTokenBucket_denyDoesNotConsumeTokens(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	tb := newTestTokenBucket(t, 1, 3, start)

	tb.AllowN(2)

	if tb.AllowN(5).Allowed {
		t.Fatal("AllowN(5): expected denied")
	}

	got := tb.Allow()
	if !got.Allowed {
		t.Fatal("expected 1 remaining token after rejected batch")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestTokenBucket_clockBackward(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(10, 5)
	tb.clock = clock
	tb.lastRefill = start

	for range 5 {
		tb.Allow()
	}
	if tb.Allow().Allowed {
		t.Fatal("expected denied at capacity")
	}

	clock.now = start.Add(-time.Second)

	got := tb.Allow()
	if got.Allowed {
		t.Fatal("expected still denied after backward clock jump")
	}
}

func TestTokenBucket_zeroRateNoRefill(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(0, 3)
	tb.clock = clock
	tb.lastRefill = start

	for range 3 {
		if !tb.Allow().Allowed {
			t.Fatal("expected initial burst")
		}
	}

	got := tb.Allow()
	if got.Allowed {
		t.Fatal("expected denied with zero rate after burst")
	}
	if got.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %v, want 0 when rate is 0", got.RetryAfter)
	}

	clock.Advance(time.Hour)
	if tb.Allow().Allowed {
		t.Fatal("expected still denied — zero rate means no refill")
	}
}

func TestTokenBucket_steadyRateAfterBurst(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(2, 4) // 2/sec, burst 4
	tb.clock = clock
	tb.lastRefill = start

	for range 4 {
		tb.Allow()
	}
	if tb.Allow().Allowed {
		t.Fatal("burst exhausted")
	}

	clock.Advance(500 * time.Millisecond) // +1 token
	if !tb.Allow().Allowed {
		t.Fatal("expected 1 token after 500ms")
	}
	if tb.Allow().Allowed {
		t.Fatal("expected denied again")
	}

	clock.Advance(500 * time.Millisecond) // +1 more
	if !tb.Allow().Allowed {
		t.Fatal("expected another token after 500ms")
	}
}

func TestTokenBucket_remainingNeverExceedsCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(100, 5)
	tb.clock = clock
	tb.lastRefill = start

	clock.Advance(time.Minute)

	got := tb.AllowN(0)
	if got.Remaining > 5 {
		t.Fatalf("Remaining = %d, must not exceed capacity 5", got.Remaining)
	}
}

func TestTokenBucket_denyCheckUsesCappedTokens(t *testing.T) {
	// Regression: uncapped refill math must not allow n > capacity after long idle.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(100, 5)
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)
	clock.Advance(time.Minute) // would compute 6000 uncapped tokens

	got := tb.AllowN(6)
	if got.Allowed {
		t.Fatal("AllowN(6) with capacity 5: must deny even after huge uncapped refill")
	}

	allowed := 0
	for range 10 {
		if tb.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("allowed %d, want 5 (capped tokens only)", allowed)
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	tb := NewTokenBucket(1000, 1000)

	var wg sync.WaitGroup
	const goroutines = 50
	const perG = 20

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perG {
				tb.Allow()
			}
		}()
	}
	wg.Wait()

	denied := 0
	for range 100 {
		if !tb.Allow().Allowed {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("expected some requests denied after concurrent burst")
	}
}

func TestTokenBucket_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race stress test in short mode")
	}
	tb := NewTokenBucket(100, 100)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tb.Allow()
			tb.AllowN(2)
		}()
	}
	wg.Wait()
}

func TestTokenBucket_retryAfterWithinTolerance(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	tb := NewTokenBucket(3, 5)
	tb.clock = clock
	tb.lastRefill = start

	drainBucket(tb, clock)

	got := tb.AllowN(2)
	if got.Allowed {
		t.Fatal("expected denied")
	}
	want := 2 * time.Second / 3 // missing 2 at 3/sec
	if math.Abs(float64(got.RetryAfter-want)) > float64(time.Millisecond) {
		t.Fatalf("RetryAfter = %v, want ~%v", got.RetryAfter, want)
	}
}
