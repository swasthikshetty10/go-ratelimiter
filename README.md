# go-ratelimiter

Thread-safe rate limiting for Go with zero external dependencies. Four algorithms, an in-memory backend, and an optional manager for per-key limits.

Requires Go 1.22+.

## Installation

```bash
go get github.com/swasthikshetty10/go-ratelimiter
```

## Quick start

```go
package main

import (
    "fmt"
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
    fmt.Printf("allowed=%v remaining=%d\n", r.Allowed, r.Remaining)
}
```

Per-user limits with the manager:

```go
import "github.com/swasthikshetty10/go-ratelimiter/manager"

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

## Features

- Fixed window, sliding window counter, token bucket, leaky bucket
- Thread-safe `Allow()` and `AllowN(n)` with `Remaining`, `Limit`, `RetryAfter`
- Functional options with validation at construction time
- Pluggable backend registry (Redis/Valkey planned)
- Optional `manager` package for key → limiter caching and idle eviction

## Packages

| Package | Purpose |
|---------|---------|
| `limiter` | Core interface, factory, options, algorithms |
| `limiter/inmemory` | In-process backend (stdlib only) |
| `manager` | Multi-tenant key lookup; use with in-memory only |

## Examples

```bash
go run ./examples/simple/              # single limiter
go run ./examples/manager/simple/      # per-user, same limit
go run ./examples/manager/quota/       # per-user, tiered limits
go run ./examples/middleware/          # HTTP middleware
```

See [examples/README.md](examples/README.md).

## Documentation

- [Architecture & algorithms](docs/ARCHITECTURE.md) — design, theory, backend extension guide
- [Examples](examples/README.md) — runnable walkthroughs

## Development

```bash
go test ./...
```
