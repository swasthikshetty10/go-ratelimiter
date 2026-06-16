# go-ratelimiter

Thread-safe rate limiting for Go with multiple algorithms, an in-memory backend, and Redis support for distributed systems.

Requires Go 1.22+.

## Installation

```bash
go get github.com/swasthikshetty10/go-ratelimiter
```

For Redis:

```bash
go get github.com/swasthikshetty10/go-ratelimiter/limiter/redis
go get github.com/redis/go-redis/v9
```

## Quick start (in-memory)

```go
package main

import (
    "log"
    "time"

    "github.com/swasthikshetty10/go-ratelimiter/limiter"
    "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
)

func main() {
    lim, err := inmemory.New(
        limiter.AlgorithmSlidingWindowCounter,
        limiter.WithLimit(100),
        limiter.WithWindow(time.Minute),
    )
    if err != nil {
        log.Fatal(err)
    }

    r := lim.Allow()
    log.Printf("allowed=%v remaining=%d\n", r.Allowed, r.Remaining)
}
```

Per-user limits with the manager (in-memory only):

```go
import (
    "github.com/swasthikshetty10/go-ratelimiter/limiter"
    "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
    "github.com/swasthikshetty10/go-ratelimiter/manager"
)

mgr, _ := manager.New(manager.Config{
    NewLimiter: func(_ string) (limiter.Limiter, error) {
        return inmemory.New(
            limiter.AlgorithmTokenBucket,
            limiter.WithRate(2),
            limiter.WithCapacity(10),
        )
    },
})

lim, _ := mgr.Get(userID) // call on every request
r := lim.Allow()
```

## Quick start (Redis)

Redis uses a separate keyed API — pass the tenant id on each call. No manager.

```go
import (
    "context"

    "github.com/redis/go-redis/v9"
    redlimiter "github.com/swasthikshetty10/go-ratelimiter/limiter/redis"
)

bucket, err := redlimiter.NewTokenBucket(
    redlimiter.WithClient(rdb),
    redlimiter.WithKeyPrefix("ratelimit:"),
    redlimiter.WithRate(10),
    redlimiter.WithCapacity(100),
)

r := bucket.AllowKey(ctx, "user:"+userID)
```

Both backends return the shared `limiter.Result` shape (`Allowed`, `Remaining`, `Limit`, `RetryAfter`).

## Features

- Algorithms: Fixed window, sliding window counter, token bucket, leaky bucket (in-memory); token bucket (Redis)
- Thread-safe admission with `Remaining`, `Limit`, `RetryAfter`
- Functional options with validation at construction time
- Optional `manager` package for in-memory per-key caching and idle eviction

## Packages

| Package            | Purpose                                                        |
| ------------------ | -------------------------------------------------------------- |
| `limiter`          | Core `Limiter` interface, `Result`, in-memory factory, options |
| `limiter/inmemory` | In-process backend (stdlib only)                               |
| `limiter/redis`    | Distributed backend; keyed `AllowKey` API (go-redis/v9)        |
| `manager`          | Multi-tenant key lookup; **in-memory only**                    |
  |


## Examples

```bash
go run ./examples/simple/              # single in-memory limiter
go run ./examples/manager/simple/      # per-user, same limit
go run ./examples/manager/quota/       # per-user, tiered limits
go run ./examples/middleware/          # HTTP middleware
go run ./examples/redis/               # Redis token bucket (needs Redis)
```

See [examples/README.md](examples/README.md).

## Documentation

- [Architecture & algorithms](docs/ARCHITECTURE.md) — design, two API surfaces, backend details
- [Examples](examples/README.md) — runnable walkthroughs

## Development

```bash
go test ./...
```

