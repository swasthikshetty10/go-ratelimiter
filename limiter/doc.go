// Package limiter provides thread-safe, in-memory rate limiting algorithms
// with zero external dependencies.
//
// Supported algorithms:
//   - FixedWindow: simple fixed time-window counter
//   - SlidingWindowCounter: O(1) approximate sliding window
//   - TokenBucket: controlled burst with steady refill rate
//   - LeakyBucket: smooth output with bounded queue depth
//
// See the repository README for usage examples and algorithm selection guidance.
package limiter
