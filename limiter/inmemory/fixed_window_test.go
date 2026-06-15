package inmemory

import (
	"sync"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

// fakeClock provides deterministic time for tests.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func newTestFixedWindow(t *testing.T, limit int, window time.Duration, start time.Time) *FixedWindow {
	t.Helper()
	fw := NewFixedWindow(limit, window)
	fw.clock = &fakeClock{now: start}
	fw.windowStart = start
	return fw
}

func TestFixedWindow_ImplementsLimiter(t *testing.T) {
	var _ limiter.Limiter = (*FixedWindow)(nil)
}

func TestFixedWindow_Allow_withinLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 3, time.Second, start)

	for i := 0; i < 3; i++ {
		got := fw.Allow()
		if !got.Allowed {
			t.Fatalf("Allow() #%d: expected allowed, got denied", i+1)
		}
		if got.Limit != 3 {
			t.Fatalf("Allow() #%d: Limit = %d, want 3", i+1, got.Limit)
		}
		wantRemaining := 3 - (i + 1)
		if got.Remaining != wantRemaining {
			t.Fatalf("Allow() #%d: Remaining = %d, want %d", i+1, got.Remaining, wantRemaining)
		}
		if got.RetryAfter != 0 {
			t.Fatalf("Allow() #%d: RetryAfter = %v, want 0 when allowed", i+1, got.RetryAfter)
		}
	}
}

func TestFixedWindow_Allow_atLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 2, time.Second, start)

	fw.Allow()
	fw.Allow()

	got := fw.Allow()
	if got.Allowed {
		t.Fatal("third Allow(): expected denied at limit")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}
	if got.RetryAfter != time.Second {
		t.Fatalf("RetryAfter = %v, want 1s until window ends", got.RetryAfter)
	}
}

func TestFixedWindow_Allow_windowReset(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(2, time.Second)
	fw.clock = clock
	fw.windowStart = start

	fw.Allow()
	fw.Allow()
	if fw.Allow().Allowed {
		t.Fatal("expected denied before window reset")
	}

	clock.Advance(time.Second)

	got := fw.Allow()
	if !got.Allowed {
		t.Fatal("expected allowed after window reset")
	}
	if got.Remaining != 1 {
		t.Fatalf("Remaining = %d, want 1 after reset", got.Remaining)
	}
}

func TestFixedWindow_Allow_exactWindowBoundary(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(1, time.Second)
	fw.clock = clock
	fw.windowStart = start

	if !fw.Allow().Allowed {
		t.Fatal("first Allow(): expected allowed")
	}
	if fw.Allow().Allowed {
		t.Fatal("second Allow(): expected denied within same window")
	}

	// Exactly at boundary: elapsed >= window triggers reset.
	clock.Advance(time.Second)

	got := fw.Allow()
	if !got.Allowed {
		t.Fatal("Allow() at window boundary: expected allowed after reset")
	}
}

func TestFixedWindow_BoundarySpike(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(5, time.Minute)
	fw.clock = clock
	fw.windowStart = start

	// Exhaust limit at end of window.
	for i := 0; i < 5; i++ {
		if !fw.Allow().Allowed {
			t.Fatalf("request %d: expected allowed", i+1)
		}
	}
	if fw.Allow().Allowed {
		t.Fatal("expected denied at limit")
	}

	// Advance just past window — counter resets; another full burst is allowed.
	clock.Advance(time.Minute)

	allowed := 0
	for i := 0; i < 6; i++ {
		if fw.Allow().Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("after reset: %d requests allowed, want 5 (demonstrates boundary burst)", allowed)
	}
}

func TestFixedWindow_AllowN_success(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 10, time.Second, start)

	got := fw.AllowN(4)
	if !got.Allowed {
		t.Fatal("AllowN(4): expected allowed")
	}
	if got.Remaining != 6 {
		t.Fatalf("Remaining = %d, want 6", got.Remaining)
	}

	got = fw.AllowN(6)
	if !got.Allowed {
		t.Fatal("AllowN(6): expected allowed")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestFixedWindow_AllowN_rejectsWithoutPartialAdmit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 5, time.Second, start)

	fw.AllowN(4)

	got := fw.AllowN(2)
	if got.Allowed {
		t.Fatal("AllowN(2) with 1 slot left: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 when denied", got.Remaining)
	}

	// Count unchanged — still 4 used.
	got = fw.Allow()
	if !got.Allowed {
		t.Fatal("expected one remaining slot still available")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0 after final admit", got.Remaining)
	}
}

func TestFixedWindow_AllowN_exceedsLimit(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 5, time.Second, start)

	got := fw.AllowN(6)
	if got.Allowed {
		t.Fatal("AllowN(6) with limit 5: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestFixedWindow_AllowN_zeroAndNegative(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fw := newTestFixedWindow(t, 5, time.Second, start)

	fw.AllowN(3)

	for _, n := range []int{0, -1} {
		got := fw.AllowN(n)
		if !got.Allowed {
			t.Fatalf("AllowN(%d): expected allowed (no-op)", n)
		}
		if got.Remaining != 2 {
			t.Fatalf("AllowN(%d): Remaining = %d, want 2 (count unchanged)", n, got.Remaining)
		}
	}
}

func TestFixedWindow_RetryAfter_decreasesOverWindow(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(1, 10*time.Second)
	fw.clock = clock
	fw.windowStart = start

	fw.Allow()
	got := fw.Allow()
	if got.RetryAfter != 10*time.Second {
		t.Fatalf("RetryAfter = %v, want 10s at window start", got.RetryAfter)
	}

	clock.Advance(3 * time.Second)
	got = fw.Allow()
	if got.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %v, want 7s after 3s elapsed", got.RetryAfter)
	}
}

func TestFixedWindow_clockBackward(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(2, time.Second)
	fw.clock = clock
	fw.windowStart = start

	fw.Allow()
	fw.Allow()
	if fw.Allow().Allowed {
		t.Fatal("expected denied at limit")
	}

	// Clock jumps backward — should not reset counter.
	clock.now = start.Add(-time.Second)

	got := fw.Allow()
	if got.Allowed {
		t.Fatal("expected still denied after backward clock jump")
	}
}

func TestFixedWindow_idleBeyondWindow(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fw := NewFixedWindow(3, time.Second)
	fw.clock = clock
	fw.windowStart = start

	fw.Allow()
	fw.Allow()

	clock.Advance(5 * time.Second)

	got := fw.Allow()
	if !got.Allowed {
		t.Fatal("expected allowed after long idle")
	}
	if got.Remaining != 2 {
		t.Fatalf("Remaining = %d, want 2 after reset with one consumed", got.Remaining)
	}
}

func TestFixedWindow_Concurrent(t *testing.T) {
	fw := NewFixedWindow(1000, time.Minute)

	var wg sync.WaitGroup
	const goroutines = 50
	const perG = 20

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perG {
				fw.Allow()
			}
		}()
	}
	wg.Wait()

	// Exact count is non-deterministic under contention, but must not exceed limit
	// without window reset. Hammer again — some must be denied if over limit.
	denied := 0
	for range 100 {
		if !fw.Allow().Allowed {
			denied++
		}
	}
	if denied == 0 {
		t.Fatal("expected some requests denied after concurrent burst")
	}
}

func TestFixedWindow_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race stress test in short mode")
	}
	fw := NewFixedWindow(100, time.Minute)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fw.Allow()
			fw.AllowN(2)
		}()
	}
	wg.Wait()
}
