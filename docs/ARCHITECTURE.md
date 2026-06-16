# Architecture

Production-quality rate limiting for Go with two deliberate API surfaces:

- **In-memory** — `limiter.Limiter` (`Allow` / `AllowN`), factory, optional `manager` for per-key caching.
- **Redis** — separate keyed API (`AllowKey` / `AllowNKey`) in `limiter/redis`; state lives in Redis, not in bound Go instances.

The in-memory backend has zero external dependencies. Redis uses [go-redis/v9](https://github.com/redis/go-redis) and shares only **`limiter.Result`** (and optionally validators / algorithm constants) with the core — not the `Limiter` interface or core factory registry.

User-facing overview: [README.md](../README.md).

## Design decision: two APIs

Backend interchangeability at the call site was deprioritized. Redis naturally stores state by key; wrapping that in a bound-key `Limiter` only to satisfy a shared interface added complexity without enough benefit.

| | In-memory | Redis |
|--|-----------|-------|
| **Admission** | `lim.Allow()` / `lim.AllowN(n)` | `lim.AllowKey(ctx, key)` / `lim.AllowNKey(ctx, key, n)` |
| **State** | Go struct per limiter | Redis key per tenant |
| **Multi-tenant** | `manager.Get(key).Allow()` | Pass key on each call (no manager) |
| **Core interface** | Implements `limiter.Limiter` | Own API in `limiter/redis` |
| **Factory** | `limiter.NewInMemory` / `inmemory.New` | `redis.NewTokenBucket(...)` (per algorithm) |
| **Shared** | — | `limiter.Result` response shape |

Applications choose a backend at integration time; handler code differs slightly between paths.

## Goals

| Goal | Approach |
|------|----------|
| Thread-safe | Per-limiter `sync.Mutex` (in-memory); atomic Lua scripts (Redis) |
| Easy to extend | One file per algorithm per backend; in-memory uses `Spec` registry |
| Clear separation | Core contract, in-memory backend, Redis backend, manager are separate |
| Idiomatic Go | Small structs, functional options, explicit constructors |
| Testable | Injectable `Clock` (in-memory); miniredis / integration tests (Redis) |
| Appropriate API per backend | `Limiter` for process-local; keyed API for Redis |

## Performance Targets

- **O(1) operations** — each admission call does constant work regardless of configured limit (excluding network latency for Redis).
- **Zero allocations on the in-memory hot path** — `Allow()` returns a stack-allocated `Result`.
- **Many in-memory instances** — fixed memory per limiter; use `manager` with `MaxIdle` / `MaxKeys` for many tenants.
- **Redis** — one round trip per `AllowKey`; no Go-side limiter cache required.

## Package Layout

```
go-ratelimiter/
├── go.mod
├── README.md
├── docs/
│   └── ARCHITECTURE.md
├── examples/
│   ├── README.md
│   ├── simple/              # single in-memory limiter
│   └── manager/
│       ├── simple/          # per-user, same quota
│       └── quota/           # per-user, tiered plans
├── limiter/                 # Core: Limiter, Result, factory (in-memory only), options
│   ├── limiter.go
│   ├── algorithm.go
│   ├── option.go
│   ├── validate.go
│   ├── factory.go           # NewInMemory + RegisterInMemory
│   ├── inmemory/            # In-process backend (zero deps)
│   │   ├── register.go
│   │   └── *_window*.go, token_bucket.go, leaky_bucket.go
│   └── redis/               # Distributed backend (go-redis/v9); separate API
│       ├── options.go
│       ├── token_bucket.go
│       └── *_test.go
└── manager/                 # In-memory multi-tenant only
    ├── manager.go
    └── ...
```

**Not planned:** registering Redis in the core `limiter` factory (`RegisterRedis` / `NewRedis`). Redis owns its constructors and options under `limiter/redis/`.

Valkey and other Redis-protocol clients work via go-redis's `redis.Scripter` interface.

## Layer model

### In-memory path

```
Application
    │
    ├─ single tenant ──► inmemory.New(...) ──► Allow() / AllowN()
    │
    └─ multi-tenant ───► manager.Get(key) ───► Allow() / AllowN()
                              │
                              ▼
                    limiter.Limiter + limiter.Result
                              │
                              ▼
                    inmemory (mutex + Clock)
```

### Redis path

```
Application
    │
    └─ per request ────► redis.TokenBucket.AllowKey(ctx, tenantKey)
                              │
                              ▼
                    limiter.Result  (shared response type)
                              │
                              ▼
                    Lua script via redis.Cmdable
                              │
                              ▼
                    Redis key = prefix + tenantKey
```

No `manager` on the Redis path — the tenant key is passed on every admission call.

## Manager (in-memory multi-tenant only)

Top-level `manager/` package — optional, not part of the core limiter contract.

```
Application derives key (user ID, API key, tenant:user)
        │
        ▼
  manager.Get(key)     ← updates lastAccess every call
        │
        ▼
  limiter.Allow()
```

| Responsibility | Owner |
|----------------|-------|
| Key format, JWT, tenant identity | Application |
| Plan / tier / limit resolution | Application (`Config.NewLimiter`) |
| Lazy create, cache, idle eviction | Manager |

### Configuration

```go
manager.Config{
    NewLimiter: func(key string) (limiter.Limiter, error) { ... },
    MaxKeys:    10_000,           // 0 = unlimited
    MaxIdle:    30 * time.Minute, // 0 = no idle eviction
}
```

### Idle eviction

TTL on **last access** only — no algorithm-specific idle checks:

- `Purge()` — sweep entries where `now - lastAccess > MaxIdle`
- `RunPurge(ctx, interval)` — background sweeper (default interval: 5 minutes)

Purge uses `CompareAndSwap` on `lastAccess` to avoid evicting a key touched concurrently during sweep.

**In-memory only.** Do not use `manager` with Redis — call `AllowKey(ctx, key)` directly.

See [examples/manager/](../examples/manager/).

## Factory and options (in-memory)

The core factory registry applies **only** to the in-memory backend.

### `Config`

Resolved parameter bag passed to every backend builder:

```go
type Config struct {
    Limit    int           // window algorithms
    Window   time.Duration // window algorithms
    Rate     float64       // bucket algorithms (units/sec)
    Capacity int           // bucket algorithms
    Clock    Clock         // in-memory only
}
```

Backend `Spec.Validate` enforces the required subset per algorithm.

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

### `Spec` registry (in-memory)

Each algorithm registers via `RegisterInMemory` in `limiter/inmemory/register.go`:

```go
type Spec struct {
    Validate func(Config) error
    Build    func(Config) (Limiter, error)
}
```

Construction flow:

1. `applyOptions(opts)` → `Config`
2. `spec.Validate(cfg)`
3. `spec.Build(cfg)` → `limiter.Limiter`

Sentinel errors: `ErrUnknownBackend`, `ErrUnknownAlgorithm`, `ErrInvalidLimit`, `ErrInvalidWindow`, `ErrInvalidRate`, `ErrInvalidCapacity`.

## Redis backend (`limiter/redis`)

Separate package with a Redis-native API. Depends on `github.com/redis/go-redis/v9`.

### API

```go
type TokenBucket struct { ... }

func NewTokenBucket(opts ...Option) (*TokenBucket, error)

func (t *TokenBucket) AllowKey(ctx context.Context, key string) limiter.Result
func (t *TokenBucket) AllowNKey(ctx context.Context, key string, n int) limiter.Result
```

- **`key`** — logical tenant id (e.g. `user:123`); combined with `WithKeyPrefix` to form the Redis key.
- **`ctx`** — passed to go-redis for timeouts and cancellation.
- **Returns `limiter.Result`** — same response shape as in-memory for HTTP headers and logging.

Optional package-local interface (not in `limiter` root):

```go
type KeyLimiter interface {
    AllowKey(ctx context.Context, key string) limiter.Result
    AllowNKey(ctx context.Context, key string, n int) limiter.Result
}
```

### Options

| Option | Purpose |
|--------|---------|
| `WithClient(c redis.Scripter)` | go-redis client, cluster, or ring (anything that runs scripts) |
| `WithKeyPrefix(prefix string)` | Prepended to every key (e.g. `ratelimit:`) |
| `WithRate(r float64)` | Units per second (delegates to `limiter.WithRate` for validation) |
| `WithCapacity(n int)` | Bucket capacity |

Redis options live in `limiter/redis`; they are not `limiter.Option` values on the core factory.

### Fail closed

When Redis cannot answer, admission **denies** the request. The library does not fail open (allow on error).

| Condition | `Result` |
|-----------|----------|
| Redis/network error (`script.Run` fails) | `Allowed: false`, `Limit: capacity` |
| Unexpected script output (`len(result) < 4`) | `Allowed: false`, `Limit: capacity` |
| Empty logical `key` (`""`) | `Allowed: false`, `Limit: capacity` |

`Remaining` and `RetryAfter` are zero in these cases. There is no error return from `AllowKey` / `AllowNKey` — callers treat `Allowed == false` like a rate-limit denial. For observability, wrap calls and log Redis errors at the application layer if needed.

Construction errors (`ErrClientRequired`, `ErrInvalidRate`, etc.) still return from `NewTokenBucket` as usual.

**Rationale:** fail closed avoids bypassing limits during outages; safer for public APIs than silently allowing traffic when Redis is down.

### Implementation notes

- **Atomicity** — token bucket uses an embedded Lua script (`EVAL` / `EVALSHA` via `redis.NewScript`).
- **Time** — server-side `TIME` in Lua; `limiter.Clock` / `WithClock` do not apply.
- **Key TTL** — Lua sets `EXPIRE` after each update: `ceil(capacity / rate) + 60` seconds so idle tenant keys are reclaimed from Redis memory.
- **Testing** — miniredis for integration-style tests; `redis.Scripter` stub for fail-closed without dial retries.
- **Valkey** — Redis-protocol compatible; use go-redis `Scripter` against Valkey in most deployments.

### Usage

```go
bucket, err := redis.NewTokenBucket(
    redis.WithClient(rdb),
    redis.WithKeyPrefix("ratelimit:"),
    redis.WithRate(10),
    redis.WithCapacity(100),
)

r := bucket.AllowKey(ctx, "user:"+userID)
if !r.Allowed {
    // 429, r.RetryAfter
}
```

Future algorithms (fixed window, sliding window counter, leaky bucket) follow the same pattern: own struct, keyed methods, Lua script, shared `limiter.Result`.

## Core abstractions

### `Limiter` (in-memory)

Process-local admission contract. Implemented by `limiter/inmemory` types only — **not** by Redis.

```go
type Limiter interface {
    Allow() Result
    AllowN(n int) Result
}
```

- `Allow()` — admit one unit (one request, one token, etc.).
- `AllowN(n)` — admit `n` units atomically; rejects the whole batch if capacity is insufficient.

Batch semantics matter for APIs that consume multiple quota units per call (e.g. bulk endpoints).

### `Result` (shared)

Used by both in-memory and Redis backends so middleware can read the same fields:

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

### `Clock` (in-memory only)

Injectable time source for deterministic in-memory tests:

```go
type Clock interface {
    Now() time.Time
}

type RealClock struct{} // wraps time.Now()
```

Algorithms in `limiter/inmemory` hold a `Clock`, defaulting to `RealClock{}`.

## Algorithm Summary

| Algorithm | Memory | Accuracy | Burst | Best for |
|-----------|--------|----------|-------|----------|
| Fixed Window | O(1) | Low at window edges | Hard burst at boundary | Simple quotas, low traffic |
| Sliding Window Counter | O(1) | High | Smooth | API rate limits (industry standard) |
| Token Bucket | O(1) | High | Controlled burst | Traffic shaping, allow spikes |
| Leaky Bucket | O(1) | High | No burst (smooth output) | Steady output rate, queue-like |

Detailed theory, tradeoffs, data structures, and edge cases are documented per algorithm below; in-memory implementations follow these structures.

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
| Hot path | In-memory: mutex + compute; Redis: one `EVALSHA` round trip |
| Keyed multi-tenant | In-memory: `manager`; Redis: `AllowKey(ctx, prefix+id)` |
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
| Factory | `limiter_test`: in-memory algorithms, unknown algo, missing fields |
| Manager | Per-key isolation, max keys, purge idle/active, concurrent Get |
| Redis | miniredis or integration: `AllowKey`, prefix, Lua correctness |
| Boundary | Window rollovers, exact limit, limit+1 |
| Fixed-window spike | N requests at end of window + N at start of next |
| Concurrent | Goroutines hammering `Allow`; clean under `-race` |

## Implementation Status

| Component | Status |
|-----------|--------|
| Core `Limiter` + `Result` + options + validation | Done |
| In-memory algorithms + tests | Done |
| In-memory factory (`NewInMemory`) | Done |
| Manager (in-memory multi-tenant) | Done |
| README + examples | Done |
| Redis token bucket (`AllowKey` / `AllowNKey`, fail closed, key TTL) | Done |
| Redis: other algorithms | Planned |

## Out of scope (core library)

- HTTP middleware (compose with `Result.RetryAfter` in your framework)
- Dynamic limit reconfiguration at runtime (in-memory: `manager.Delete(key)`; Redis: key TTL or app policy)
- Metrics / observability hooks (wrap admission calls in your service)
- Forcing Redis to implement `limiter.Limiter` or the in-memory factory registry
