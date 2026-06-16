# Architecture

Production-quality rate limiting for Go: a small core contract, functional options, and pluggable backends. The in-memory backend ships with zero external dependencies; distributed backends (Redis, Valkey, etc.) plug in via the same `Limiter` interface and factory pattern.

## Goals

| Goal | Approach |
|------|----------|
| Thread-safe | Per-limiter `sync.Mutex` (in-memory); atomic Lua/scripts (distributed backends) |
| Easy to extend | `Limiter` interface + `Spec` registry per backend + one file per algorithm |
| Clear separation | Core contract, factory/options, backend implementations are distinct packages |
| Idiomatic Go | Small structs, functional options, blank-import registration via `init` |
| Testable | Injectable `Clock` on in-memory backend; fake clocks in unit tests |
| Backend-agnostic API | Applications depend on `limiter.Limiter`, not concrete types |

## Performance Targets

- **O(1) operations** — each `Allow()` / `AllowN()` does constant work regardless of configured limit or request history.
- **Zero allocations on the hot path** — `Allow()` returns a stack-allocated `Result`; no heap allocations in steady state.
- **Many instances** — fixed memory per limiter (a handful of fields plus one mutex); suitable for thousands of independent limiter instances in one process without per-request storage growth.

## Package Layout

```
go-ratelimiter/
├── go.mod
├── docs/
│   └── ARCHITECTURE.md
├── examples/
│   └── main.go
└── limiter/                         # Core: contract, factory, options, validation
    ├── limiter.go                   # Limiter, Result, Clock
    ├── algorithm.go                 # Algorithm constants
    ├── option.go                    # WithLimit, WithWindow, WithRate, WithCapacity, WithClock
    ├── validate.go                  # Shared validators + sentinel errors
    ├── factory.go                   # Backend registry, NewInMemory, RegisterInMemory
    ├── factory_test.go
    ├── doc.go
    └── inmemory/                    # In-process backend (zero deps)
        ├── register.go              # init() → RegisterInMemory per algorithm
        ├── fixed_window.go
        ├── sliding_window_counter.go
        ├── token_bucket.go
        ├── leaky_bucket.go
        └── *_test.go
```

Future backends follow the same shape without touching core or in-memory code:

```
limiter/redis/       # RegisterRedis + NewRedis (future)
limiter/valkey/      # RegisterValkey + NewValkey (future)
```

Each backend package owns its `init()` registration and algorithm-specific client wiring. Algorithm implementations live in the backend package because distributed limiters embed network I/O and key semantics that in-memory limiters do not share.

## Layer Model

```
┌─────────────────────────────────────────────────────────┐
│  Application / HTTP middleware / gRPC interceptor       │
└──────────────────────────┬──────────────────────────────┘
                           │ Allow() / AllowN()
┌──────────────────────────▼──────────────────────────────┐
│  limiter.Limiter  +  limiter.Result                     │  ← stable contract
└──────────────────────────┬──────────────────────────────┘
                           │ built by
┌──────────────────────────▼──────────────────────────────┐
│  Factory: NewInMemory(algo, opts...)                    │
│  Options: WithLimit, WithWindow, WithRate, ...          │
│  Config  →  Spec.Validate  →  Spec.Build                │
└──────────────────────────┬──────────────────────────────┘
                           │ implements (per backend)
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   inmemory           redis (future)     valkey (future)
   FixedWindow        same algorithms    same algorithms
   SlidingWindow      over TCP/Lua       over TCP/Lua
   TokenBucket
   LeakyBucket
        │
        ▼ (in-memory only)
      Clock
```

Applications import the backend they need with a blank import, then call the matching constructor. The hot path is always `Limiter.Allow()` — no factory involvement after construction.

## Factory and Options

### `Config`

Resolved parameter bag passed to every backend builder:

```go
type Config struct {
    Limit    int           // window algorithms
    Window   time.Duration // window algorithms
    Rate     float64       // bucket algorithms (units/sec)
    Capacity int           // bucket algorithms
    Clock    Clock         // in-memory only; ignored by distributed backends
}
```

Not every field applies to every algorithm. Backend `Spec.Validate` enforces the required subset.

### `Option`

Functional options validate eagerly when applied:

```go
l, err := limiter.NewInMemory(
    limiter.AlgorithmSlidingWindowCounter,
    limiter.WithLimit(100),
    limiter.WithWindow(time.Minute),
    limiter.WithClock(testClock), // optional
)
```

| Option | Used by |
|--------|---------|
| `WithLimit` + `WithWindow` | Fixed Window, Sliding Window Counter |
| `WithRate` + `WithCapacity` | Token Bucket, Leaky Bucket |
| `WithClock` | In-memory backends only |

### `Spec` registry

Each `(Backend, Algorithm)` pair registers a validator and builder:

```go
type Spec struct {
    Validate func(Config) error
    Build    func(Config) (Limiter, error)
}
```

Registration happens in backend `init()` via `RegisterInMemory` (and future `RegisterRedis`, etc.). The core package holds a two-level map: `registries[Backend][Algorithm]`.

Construction flow:

1. `applyOptions(opts)` → `Config` (per-option validation)
2. `spec.Validate(cfg)` → algorithm-required fields present
3. `spec.Build(cfg)` → concrete `Limiter`

Sentinel errors: `ErrUnknownBackend`, `ErrUnknownAlgorithm`, `ErrInvalidLimit`, `ErrInvalidWindow`, `ErrInvalidRate`, `ErrInvalidCapacity`.

### Adding a distributed backend

To add Redis (same pattern for Valkey):

1. Create `limiter/redis/` with a `Limiter` implementation per algorithm (Lua script or `INCR` + `PEXPIRE`).
2. In `init()`, call `RegisterRedis(algo, Spec{Validate, Build})` — mirror `RegisterInMemory`.
3. Expose `NewRedis(algo, opts ...Option)` delegating to `newLimiter(BackendRedis, ...)`.
4. Add backend-specific options in the redis package (e.g. `WithClient`, `WithKey`) that wrap or extend `Config` before `Build`.

Distributed implementations **must** return the same `Result` semantics documented below so middleware works unchanged. `WithClock` does not apply; time comes from the server.

## Core Abstractions

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

### Data Structure (inmemory)

```go
type FixedWindow struct {
    mu          sync.Mutex
    limit       int
    window      time.Duration
    count       int
    windowStart time.Time
    clock       limiter.Clock
}
```

### Concurrency

Single mutex guards `count` and `windowStart`. All mutations happen inside the lock. No lock-free path needed — critical section is tiny.

### Edge Cases

- **Window rollover**: on `Allow`, if `now - windowStart >= window`, reset `count = 0` and set `windowStart = truncate(now, window)` (or `now` for simplicity).
- **AllowN(n) where n > limit**: always reject; never partial admit.
- **Zero or negative limit/window**: rejected by `WithLimit` / `WithWindow` or `ValidateLimitAndWindow`.
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

### Data Structure (inmemory)

```go
type SlidingWindowCounter struct {
    mu          sync.Mutex
    limit       int
    window      time.Duration
    currCount   float64
    prevCount   float64
    windowStart time.Time
    clock       limiter.Clock
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

### Data Structure (inmemory)

```go
type TokenBucket struct {
    mu         sync.Mutex
    rate       float64
    capacity   int
    tokens     float64
    lastRefill time.Time
    clock      limiter.Clock
}
```

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

### Data Structure (inmemory)

```go
type LeakyBucket struct {
    mu         sync.Mutex
    rate       float64
    capacity   int
    currVolume float64
    lastLeak   time.Time
    clock      limiter.Clock
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
| Hot path | `Allow()` lock → compute → unlock (in-memory); single round-trip (distributed) |
| Keyed multi-tenant | Per-key limiter instances or key prefix in distributed backend — outside core |
| `sync.RWMutex` | Not used — writes dominate; RWMutex adds complexity with no win here |

**Why `sync.Mutex` instead of atomics?** Each `Allow()` reads the clock, updates time-derived state (refill, leak, or window rollover), and writes several fields as one logical decision. Atomics guard single-word updates, not that full read–compute–write sequence; emulating it with CAS loops on floats and timestamps adds complexity without benefit here. The critical section is a few arithmetic operations under the lock — contention is per-instance, so mutex overhead stays negligible compared to the work of correct time-based accounting.

## Validation

Validation is two-stage:

1. **Option time** — `WithLimit`, `WithWindow`, `WithRate`, `WithCapacity` reject invalid individual values when the option is applied.
2. **Build time** — `Spec.Validate` ensures algorithm-required fields are set:

| Algorithm | Validator |
|-----------|-----------|
| Fixed Window, Sliding Window Counter | `ValidateLimitAndWindow` |
| Token Bucket, Leaky Bucket | `ValidateRateAndCapacity` |

Direct constructors in `inmemory` (e.g. `NewFixedWindow(limit, window, clock)`) accept pre-validated values; the factory is the recommended entry point for applications.

## Testing Strategy

| Category | Approach |
|----------|----------|
| Unit | Fake `Clock` in `inmemory`; assert `Allow`/`AllowN` sequences |
| Factory | `limiter_test` package: all algorithms, unknown algo, missing fields |
| Boundary | Window rollovers, exact limit, limit+1 |
| Fixed-window spike | N requests at end of window + N at start of next |
| Concurrent | Goroutines hammering `Allow`; clean under `-race` |
| Benchmarks | `Allow()` and `AllowN(10)` per algorithm; parallel variant |

## Implementation Status

| Step | Status |
|------|--------|
| Architecture | Done |
| Interface + options + validation | Done |
| In-memory algorithms + tests | Done |
| Factory + backend registry | Done |
| README | Pending |
| Distributed backends (Redis, Valkey) | Planned — extension path documented above |

## Out of Scope (core library)

- HTTP middleware (compose with `Result.RetryAfter` in your framework)
- Dynamic limit reconfiguration at runtime
- Metrics / observability hooks (wrap `Limiter` in your service)

Distributed backends are **not** out of scope — they are added as sibling packages under `limiter/` using the same `Spec` pattern, without changing the core contract.
