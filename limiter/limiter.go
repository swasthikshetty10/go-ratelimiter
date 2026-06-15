package limiter

import "time"

type Limiter interface {
	Allow() Result
	AllowN(n int) Result
}

type Result struct {
	Allowed    bool
	Remaining  int
	Limit      int
	RetryAfter time.Duration
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}
