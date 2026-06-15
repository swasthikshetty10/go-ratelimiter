# Architecture

Production-quality, in-memory rate limiting for Go with zero external dependencies.

## Goals

| Goal | Approach |
|------|----------|
| Thread-safe | Per-limiter `sync.Mutex`; no global locks |
| Easy to extend | Small interface + one file per algorithm |
| Clear separation | Core contract, algorithms, and optional keyed wrapper are distinct layers |
| Idiomatic Go | Small structs, explicit constructors, no framework magic |
| Testable | Deterministic time via injectable clock (Step 2) |

## Performance Targets

- **O(1) operations** — each `Allow()` / `AllowN()` does constant work regardless of configured limit or request history.
- **Zero allocations on the hot path** — `Allow()` returns a stack-allocated `Result`; no heap allocations in steady state.
- **Many instances** — fixed memory per limiter (a handful of fields plus one mutex); suitable for thousands of independent limiter instances in one process without per-request storage growth.

## Package Layout

```
go-ratelimiter/
├── go.mod
├── docs/
│   └── ARCHITECTURE.md          # This document
├── limiter/
│   ├── limiter.go               # Core interfaces, Result, Clock
│   ├── fixed_window.go          # Fixed Window
│   ├── sliding_window.go        # Sliding Window Counter
│   ├── token_bucket.go          # Token Bucket
│   └── leaky_bucket.go          # Leaky Bucket
├── limiter/*_test.go            # Unit, boundary, concurrent tests
├── limiter/*_bench_test.go      # Benchmarks
└── README.md                    # Usage guide (Step 8)
```

Single package (`limiter`) keeps the API flat. Each algorithm lives in its own file so new algorithms can be added without touching existing ones.

## Layer Model

```
┌─────────────────────────────────────────────────────────┐
│  Application / HTTP middleware / gRPC interceptor       │
└──────────────────────────┬──────────────────────────────┘
                           │ Allow() / AllowN()
┌──────────────────────────▼──────────────────────────────┐
│  Limiter interface (contract)                           │
│  + Result (allowed, remaining, retry-after, limit)      │
└──────────────────────────┬──────────────────────────────┘
                           │ implements
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
  FixedWindow      SlidingWindowCounter   TokenBucket
        │                  │                  │
        └──────────────────┼──────────────────┘
                           ▼
                     LeakyBucket
                           │
┌──────────────────────────▼──────────────────────────────┐
│  Clock (time source — real or fake for tests)           │
└─────────────────────────────────────────────────────────┘
```

**Optional future layer** (not in initial scope): a keyed `Manager` that maps string keys to limiter instances with LRU eviction. Algorithms stay unchanged; the manager is a thin wrapper.

## Core Abstractions (Step 2)

### `Limiter`

The primary contract every algorithm implements:

```go
type Limiter interface {
    Allow() Result
    AllowN(n int) Result
}
```

- `Allow()` — admit one unit (one request, one token, etc.).
- `AllowN(n)` — admit `n` units atomically; rejects the whole batch if capacity is insufficient.

Batch semantics matter for APIs that consume multiple quota units per call (e.g. bulk endpoints).

### `Result`

Rich enough for HTTP middleware without leaking algorithm internals:

```go
type Result struct {
    Allowed    bool
    Remaining  int     // units left in current window/bucket (best-effort per algorithm)
    Limit      int     // configured capacity
    RetryAfter time.Duration // wait hint when Allowed == false; zero when allowed
}
```

**`Remaining`** — units the caller can still consume **after this call**, without being denied on the next single-unit request. Window algorithms report headroom against the current or estimated window count; bucket algorithms report available tokens or free queue capacity (typically floored to an integer). When `Allowed == false`, `Remaining` is `0`. SlidingWindowCounter may round the estimate slightly; treat as advisory, not a hard guarantee.

**`RetryAfter`** — when `Allowed == false`, the minimum duration to wait before a single-unit `Allow()` might succeed (best-effort). Fixed/sliding window limiters usually set this to time until the window rolls or estimated load drops; token and leaky bucket limiters compute time until enough refill or leak occurs. When `Allowed == true`, `RetryAfter` is `0`. Suitable for mapping to HTTP `Retry-After` or client backoff; not a promise that the next call will succeed if other goroutines consume quota first.

### `Clock`

Injectable time source for deterministic tests:

```go
type Clock interface {
    Now() time.Time
}

type RealClock struct{} // wraps time.Now()
```

Algorithms hold a `Clock`, defaulting to `RealClock{}`. Tests use a manual/advancing fake clock.

## Algorithm Summary

| Algorithm | Memory | Accuracy | Burst | Best for |
|-----------|--------|----------|-------|----------|
| Fixed Window | O(1) | Low at window edges | Hard burst at boundary | Simple quotas, low traffic |
| Sliding Window Counter | O(1) | High | Smooth | API rate limits (industry standard) |
| Token Bucket | O(1) | High | Controlled burst | Traffic shaping, allow spikes |
| Leaky Bucket | O(1) | High | No burst (smooth output) | Steady output rate, queue-like |

Detailed theory, tradeoffs, data structures, and edge cases are documented per algorithm below and will be reflected in source comments during implementation.

---

## 1. Fixed Window

### Theory

Divide time into non-overlapping windows of fixed duration `W`. Allow at most `N` requests per window. When the clock crosses a window boundary, the counter resets to zero.

```
Window 1          Window 2          Window 3
|---- N max ----|---- N max ----|---- N max ----|
  ●●●●●●●●●●●     ●●●●●●●●●●●     ...
```

### Tradeoffs

| Pros | Cons |
|------|------|
| Simplest implementation | **Boundary spike**: up to 2×N requests can pass in a short interval straddling two windows |
| O(1) time and space | No burst control within a window |
| Easy to reason about | Less fair than sliding approaches |

### Data Structure

```go
type FixedWindow struct {
    mu       sync.Mutex
    limit    int
    window   time.Duration
    count    int
    windowStart time.Time
    clock    Clock
}
```

### Concurrency

Single mutex guards `count` and `windowStart`. All mutations happen inside the lock. No lock-free path needed — critical section is tiny.

### Edge Cases

- **Window rollover**: on `Allow`, if `now - windowStart >= window`, reset `count = 0` and set `windowStart = truncate(now, window)` (or `now` for simplicity).
- **AllowN(n) where n > limit**: always reject; never partial admit.
- **Zero or negative limit**: constructor returns error or panics (prefer error — decided in Step 2).
- **Zero window duration**: invalid; reject at construction.
- **Clock going backwards**: treat as same window (do not reset counter on backward jump).

---

## 2. Sliding Window Counter

### Theory

Hybrid of fixed window and sliding window log. Track the **previous** window count and **current** window count. Estimate the sliding-window rate as a weighted sum:

```
estimated = prevCount × (1 - elapsed/window) + currCount
```

Where `elapsed` is time into the current window. This avoids storing per-request timestamps (O(1) memory) while smoothing boundary spikes.

```
     prev window          curr window
|████████████████|████████████████|
                  ^ now
                  └── weight decays prev contribution
```

### Why counter, not log?

The **sliding window log** stores one timestamp per admitted request and counts entries in `[now − window, now]`. It is exact, but memory and per-check work scale with **O(N)** where N is the number of requests in the window — pruning, slicing, or scanning on every `Allow()`.

The **sliding window counter** keeps two counts and one window boundary — **O(1)** memory and **O(1)** work per call. It trades exactness for a small, bounded approximation at window edges. That tradeoff fits an in-memory library holding many limiter instances: per-instance cost stays fixed whether the limit is 10 or 10,000.

### Tradeoffs

| Pros | Cons |
|------|------|
| O(1) memory vs O(N) for sliding window log | Approximation, not exact sliding window |
| Smooths fixed-window spikes | Slightly more complex than fixed window |
| Exact log impractical at high limits or many keys | Two windows of history needed at boundaries |

### Data Structure

```go
type SlidingWindowCounter struct {
    mu          sync.Mutex
    limit       int
    window      time.Duration
    currCount   int
    prevCount   int
    windowStart time.Time
    clock       Clock
}
```

### Concurrency

Same mutex pattern as Fixed Window. Window rollover moves `currCount → prevCount`, resets `currCount`.

### Edge Cases

- **First window**: `prevCount = 0`.
- **Idle longer than one window**: both counts should be zero (rollover may need multiple window steps or direct reset if `now - windowStart >= 2*window`).
- **AllowN**: check estimated count + n <= limit before incrementing.
- **Floating estimate**: use integer math with careful rounding, or compare using `float64` internally (document precision tradeoff — prefer integer microsecond math for determinism).

---

## 3. Token Bucket

### Theory

A bucket holds at most `capacity` tokens. Tokens refill continuously at `rate` tokens per second. Each admitted request consumes one token. Allows **controlled bursts** up to `capacity`, then throttles to `rate`.

```
capacity ──────────────── max burst
    │    ╱╲    ╱╲
    │   ╱  ╲  ╱  ╲   ← refill at constant rate
    │  ╱    ╲╱    ╲
    └────────────────── time
         ↑ requests drain tokens
```

### Tradeoffs

| Pros | Cons |
|------|------|
| Allows controlled bursts | Harder to express "exactly N per minute" without tuning rate + capacity |
| Industry standard (network QoS) | `RetryAfter` requires computing time until enough tokens refill |
| Smooth long-term rate | Two knobs (rate, capacity) can confuse users |

### Data Structure

```go
type TokenBucket struct {
    mu       sync.Mutex
    rate     float64       // tokens per second (or use rational: tokens + interval)
    capacity int
    tokens   float64       // current token count (fractional refill is correct)
    lastRefill time.Time
    clock    Clock
}
```

Alternative: store `rate` as `int` tokens per `time.Duration` refill interval to avoid float drift — evaluate in Step 5.

### Concurrency

Refill and deduct under one mutex. Refill formula:

```
elapsed = now - lastRefill
tokens = min(capacity, tokens + elapsed.Seconds() * rate)
```

### Edge Cases

- **Initial state**: start full (`tokens = capacity`) or empty — document choice (full is typical for "allow initial burst").
- **AllowN(n)**: require `tokens >= n`; deduct atomically.
- **Very long idle**: cap tokens at `capacity` (no unbounded accumulation beyond burst).
- **Zero rate**: treat as no refill (only initial burst if capacity > 0).

---

## 4. Leaky Bucket

### Theory

Requests enter a bucket with fixed **capacity**. The bucket **leaks** at a constant rate (one drip per interval). If the bucket is full, new requests are rejected. Output rate is smooth — **no bursts** on the output side.

```
  requests in ──► [████████░░] ──► steady leak at rate R
                      ↑ full = reject
```

Distinct from Token Bucket: leaky bucket limits how much **queued** work can accumulate; output is always smooth. Token bucket allows bursting **on admission**.

### Tradeoffs

| Pros | Cons |
|------|------|
| Perfectly smooth output rate | No admission burst (stricter) |
| Natural back-pressure model | Less common for HTTP rate limiting than token bucket |
| Simple queue-depth semantics | "Leaky" vs "token" confusion in naming |

### Data Structure

```go
type LeakyBucket struct {
    mu       sync.Mutex
    rate     float64       // leaks per second (or rational interval)
    capacity int           // max queued volume
    volume   float64       // current fill level
    lastLeak time.Time
    clock    Clock
}
```

On `Allow`: leak first, then if `volume + n <= capacity`, add and allow; else reject.

### Concurrency

Same mutex + time-based leak pattern as Token Bucket.

### Edge Cases

- **Leak before admit**: always drain `volume` by `elapsed * rate` (floored at 0) before checking capacity.
- **AllowN(n) where n > capacity**: always reject.
- **Zero capacity**: reject all (or invalid config at construction).

---

## Concurrency Strategy

| Concern | Decision |
|---------|----------|
| Lock granularity | One `sync.Mutex` per limiter instance — no shared state between instances |
| Lock ordering | N/A (no nested locks) |
| Hot path | `Allow()` lock → compute → unlock; no I/O, no allocations |
| Keyed multi-tenant | Future `Manager` with `sync.Map` or sharded mutexes — out of scope for v1 |
| `sync.RWMutex` | Not used — writes dominate; RWMutex adds complexity with no win here |

**Why `sync.Mutex` instead of atomics?** Each `Allow()` reads the clock, updates time-derived state (refill, leak, or window rollover), and writes several fields as one logical decision. Atomics guard single-word updates, not that full read–compute–write sequence; emulating it with CAS loops on floats and timestamps adds complexity without benefit here. The critical section is a few arithmetic operations under the lock — contention is per-instance, so mutex overhead stays negligible compared to the work of correct time-based accounting.

## Validation (Constructors)

All constructors follow:

```go
func NewFixedWindow(limit int, window time.Duration, opts ...Option) (*FixedWindow, error)
```

Shared validation (internal helper):

- `limit > 0`
- `window > 0` (where applicable)
- `rate > 0` (token/leaky bucket)

Functional options (optional, Step 2):

```go
WithClock(c Clock)
```

## Testing Strategy (Step 7)

| Category | Approach |
|----------|----------|
| Unit | Fake clock; assert Allow/AllowN sequences |
| Boundary | Window rollovers, exact limit, limit+1 |
| Fixed-window spike | N requests at end of window + N at start of next |
| Concurrent | `testing.B` + goroutines hammering Allow; no race (`-race`) |
| Benchmarks | `Allow()` and `AllowN(10)` per algorithm; parallel benchmark variant |

## Implementation Order

1. **Architecture** ← current step
2. Interface design (`limiter.go`, `Clock`, `Result`, options, validation)
3. Fixed Window — simplest, validates the contract
4. Sliding Window Counter — builds on window rollover patterns
5. Token Bucket — introduces continuous refill math
6. Leaky Bucket — mirror of token bucket with inverted semantics
7. Tests and benchmarks
8. README with usage examples and algorithm selection guide

## Non-Goals (v1)

- Distributed / Redis-backed limiters
- HTTP middleware (users compose with `Result.RetryAfter`)
- Dynamic limit reconfiguration at runtime
- Metrics / observability hooks (easy to wrap later)

These can be added as separate packages without changing algorithm internals.
