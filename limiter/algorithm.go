package limiter

// Algorithm identifies a rate limiting strategy.
type Algorithm string

const (
	AlgorithmFixedWindow          Algorithm = "fixed_window"
	AlgorithmSlidingWindowCounter Algorithm = "sliding_window_counter"
	AlgorithmTokenBucket          Algorithm = "token_bucket"
	AlgorithmLeakyBucket          Algorithm = "leaky_bucket"
)

func (a Algorithm) String() string { return string(a) }
