package inmemory

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

func newTestLeakyBucket(t *testing.T, rate float64, capacity int, start time.Time) *LeakyBucket {
	t.Helper()
	lb := NewLeakyBucket(rate, capacity)
	lb.clock = &fakeClock{now: start}
	lb.lastLeak = start
	return lb
}

func fillLeakyBucket(lb *LeakyBucket, capacity int) {
	for range capacity {
		if !lb.Allow().Allowed {
			panic("failed to fill leaky bucket")
		}
	}
}

func TestLeakyBucket_ImplementsLimiter(t *testing.T) {
	var _ limiter.Limiter = (*LeakyBucket)(nil)
}

func TestLeakyBucket_startsEmpty(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 10, 5, start)

	got := lb.AllowN(0)
	if got.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5 (empty bucket)", got.Remaining)
	}
	if got.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", got.Limit)
	}
}

func TestLeakyBucket_fillToCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 5, start)

	for i := range 5 {
		got := lb.Allow()
		if !got.Allowed {
			t.Fatalf("Allow() #%d: expected allowed", i+1)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("RetryAfter = %v, want 0 when allowed", got.RetryAfter)
		}
		wantRemaining := 4 - i
		if got.Remaining != wantRemaining {
			t.Fatalf("Allow() #%d: Remaining = %d, want %d", i+1, got.Remaining, wantRemaining)
		}
	}

	got := lb.Allow()
	if got.Allowed {
		t.Fatal("6th Allow(): expected denied when full")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}
}

func TestLeakyBucket_leakOverTime(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(10, 5) // 10 units/sec leak
	lb.clock = clock
	lb.lastLeak = start

	fillLeakyBucket(lb, 5)

	got := lb.Allow()
	if got.Allowed {
		t.Fatal("expected denied when full")
	}
	wantRetry := 100 * time.Millisecond
	if math.Abs(float64(got.RetryAfter-wantRetry)) > float64(time.Millisecond) {
		t.Fatalf("RetryAfter = %v, want ~100ms for 1 unit at 10/s", got.RetryAfter)
	}

	clock.Advance(100 * time.Millisecond)

	got = lb.Allow()
	if !got.Allowed {
		t.Fatal("expected allowed after 100ms leak")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 after filling last slot", got.Remaining)
	}
}

func TestLeakyBucket_longIdleDrainsToEmpty(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(10, 5)
	lb.clock = clock
	lb.lastLeak = start

	fillLeakyBucket(lb, 5)
	clock.Advance(time.Second) // leak 10 units, volume floored at 0

	got := lb.AllowN(0)
	if got.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5 after full drain", got.Remaining)
	}

	allowed := 0
	for range 6 {
		if lb.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("allowed %d, want 5 after drain", allowed)
	}
}

func TestLeakyBucket_AllowN_success(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 10, start)

	got := lb.AllowN(4)
	if !got.Allowed {
		t.Fatal("AllowN(4): expected allowed")
	}
	if got.Remaining != 6 {
		t.Fatalf("Remaining = %d, want 6", got.Remaining)
	}

	got = lb.AllowN(6)
	if !got.Allowed {
		t.Fatal("AllowN(6): expected allowed")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestLeakyBucket_AllowN_rejectsWithoutPartialAdmit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 5, start)

	lb.AllowN(4)

	got := lb.AllowN(2)
	if got.Allowed {
		t.Fatal("AllowN(2) with 1 slot left: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}

	got = lb.Allow()
	if !got.Allowed {
		t.Fatal("expected one remaining slot still available")
	}
}

func TestLeakyBucket_AllowN_exceedsCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 5, start)

	got := lb.AllowN(6)
	if got.Allowed {
		t.Fatal("AllowN(6) with capacity 5: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestLeakyBucket_AllowN_zeroAndNegative(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 5, start)

	lb.AllowN(2)

	for _, n := range []int{0, -1} {
		got := lb.AllowN(n)
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

func TestLeakyBucket_retryAfterScalesWithExcess(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(2, 10)
	lb.clock = clock
	lb.lastLeak = start

	fillLeakyBucket(lb, 10)

	got := lb.AllowN(4)
	if got.Allowed {
		t.Fatal("expected denied when full")
	}
	// excess = 10 + 4 - 10 = 4 units; at 2/s → 2s
	want := 2 * time.Second
	if math.Abs(float64(got.RetryAfter-want)) > float64(time.Millisecond) {
		t.Fatalf("RetryAfter = %v, want ~2s", got.RetryAfter)
	}
}

func TestLeakyBucket_fractionalLeak(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(10, 5)
	lb.clock = clock
	lb.lastLeak = start

	fillLeakyBucket(lb, 5)

	clock.Advance(50 * time.Millisecond) // leak 0.5 — still full
	if lb.Allow().Allowed {
		t.Fatal("expected denied with 0.5 units leaked")
	}

	clock.Advance(50 * time.Millisecond) // total 100ms → 1 unit leaked
	if !lb.Allow().Allowed {
		t.Fatal("expected allowed after 1 unit leaked")
	}
}

func TestLeakyBucket_denyDoesNotAddVolume(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 3, start)

	lb.AllowN(2)

	if lb.AllowN(5).Allowed {
		t.Fatal("AllowN(5): expected denied")
	}

	got := lb.Allow()
	if !got.Allowed {
		t.Fatal("expected 1 remaining slot after rejected batch")
	}
}

func TestLeakyBucket_clockBackward(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(10, 5)
	lb.clock = clock
	lb.lastLeak = start

	fillLeakyBucket(lb, 5)
	if lb.Allow().Allowed {
		t.Fatal("expected denied when full")
	}

	clock.now = start.Add(-time.Second)

	got := lb.Allow()
	if got.Allowed {
		t.Fatal("expected still denied after backward clock jump")
	}
}

func TestLeakyBucket_zeroRateNoLeak(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(0, 3)
	lb.clock = clock
	lb.lastLeak = start

	for range 3 {
		if !lb.Allow().Allowed {
			t.Fatal("expected to fill bucket")
		}
	}

	got := lb.Allow()
	if got.Allowed {
		t.Fatal("expected denied when full")
	}
	if got.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %v, want 0 when rate is 0", got.RetryAfter)
	}

	clock.Advance(time.Hour)
	if lb.Allow().Allowed {
		t.Fatal("expected still denied — zero rate means no leak")
	}
}

func TestLeakyBucket_steadyAdmissionAfterFill(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	lb := NewLeakyBucket(2, 4) // 2 units/sec leak, capacity 4
	lb.clock = clock
	lb.lastLeak = start

	for range 4 {
		lb.Allow()
	}
	if lb.Allow().Allowed {
		t.Fatal("bucket full")
	}

	clock.Advance(500 * time.Millisecond) // leak 1
	if !lb.Allow().Allowed {
		t.Fatal("expected 1 admission after 500ms")
	}
	if lb.Allow().Allowed {
		t.Fatal("expected denied again")
	}

	clock.Advance(500 * time.Millisecond)
	if !lb.Allow().Allowed {
		t.Fatal("expected another admission after 500ms")
	}
}

func TestLeakyBucket_inputBurstUpToCapacity(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 100, start)

	// Leaky bucket allows immediate burst IN up to capacity (unlike output rate).
	allowed := 0
	for range 100 {
		if lb.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 100 {
		t.Fatalf("immediate burst: %d allowed, want 100", allowed)
	}
	if lb.Allow().Allowed {
		t.Fatal("101st request should be denied")
	}
}

func TestLeakyBucket_remainingNeverNegative(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	lb := newTestLeakyBucket(t, 1, 3, start)

	for i := range 3 {
		got := lb.Allow()
		if got.Remaining < 0 {
			t.Fatalf("Remaining = %d, must not be negative", got.Remaining)
		}
		if got.Remaining != 2-i {
			t.Fatalf("Allow() #%d: Remaining = %d, want %d", i+1, got.Remaining, 2-i)
		}
	}
}

func TestLeakyBucket_Concurrent(t *testing.T) {
	lb := NewLeakyBucket(1000, 1000)

	var wg sync.WaitGroup
	const goroutines = 50
	const perG = 20

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perG {
				lb.Allow()
			}
		}()
	}
	wg.Wait()

	denied := 0
	for range 100 {
		if !lb.Allow().Allowed {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("expected some requests denied after concurrent burst")
	}
}

func TestLeakyBucket_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race stress test in short mode")
	}
	lb := NewLeakyBucket(100, 100)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lb.Allow()
			lb.AllowN(2)
		}()
	}
	wg.Wait()
}
