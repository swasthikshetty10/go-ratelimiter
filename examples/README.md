# Examples

| Directory | Command | When to use |
|-----------|---------|-------------|
| [simple/](simple/) | `go run ./examples/simple/` | One shared limiter (no manager) |
| [manager/simple/](manager/simple/) | `go run ./examples/manager/simple/` | Per-user limits, same quota for all |
| [manager/quota/](manager/quota/) | `go run ./examples/manager/quota/` | Per-user limits, free / basic / pro tiers |
| [middleware/](middleware/) | `go run ./examples/middleware/` | HTTP middleware with 429 and rate-limit headers |

See [manager/README.md](manager/README.md) for manager-specific notes.
