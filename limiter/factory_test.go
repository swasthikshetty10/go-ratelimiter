package limiter_test

import (
	"errors"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	_ "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
)

func TestNewInMemory_FixedWindow(t *testing.T) {
	l, err := limiter.NewInMemory(
		limiter.AlgorithmFixedWindow,
		limiter.WithLimit(5),
		limiter.WithWindow(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if got := l.Allow(); !got.Allowed {
			t.Fatalf("request %d: want allowed", i+1)
		}
	}
	if got := l.Allow(); got.Allowed {
		t.Fatal("6th request: want denied")
	}
}

func TestNewInMemory_SlidingWindowCounter(t *testing.T) {
	l, err := limiter.NewInMemory(
		limiter.AlgorithmSlidingWindowCounter,
		limiter.WithLimit(3),
		limiter.WithWindow(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Allow(); !got.Allowed || got.Limit != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestNewInMemory_TokenBucket(t *testing.T) {
	l, err := limiter.NewInMemory(
		limiter.AlgorithmTokenBucket,
		limiter.WithRate(10),
		limiter.WithCapacity(5),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Allow(); !got.Allowed || got.Limit != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestNewInMemory_LeakyBucket(t *testing.T) {
	l, err := limiter.NewInMemory(
		limiter.AlgorithmLeakyBucket,
		limiter.WithRate(10),
		limiter.WithCapacity(5),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Allow(); !got.Allowed || got.Limit != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestNewInMemory_UnknownAlgorithm(t *testing.T) {
	_, err := limiter.NewInMemory(
		limiter.Algorithm("unknown"),
		limiter.WithLimit(1),
		limiter.WithWindow(time.Second),
	)
	if !errors.Is(err, limiter.ErrUnknownAlgorithm) {
		t.Fatalf("got err %v, want ErrUnknownAlgorithm", err)
	}
}

func TestNewInMemory_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		algo limiter.Algorithm
		opts []limiter.Option
		want error
	}{
		{
			name: "fixed window missing limit",
			algo: limiter.AlgorithmFixedWindow,
			opts: []limiter.Option{limiter.WithWindow(time.Second)},
			want: limiter.ErrInvalidLimit,
		},
		{
			name: "token bucket missing rate",
			algo: limiter.AlgorithmTokenBucket,
			opts: []limiter.Option{limiter.WithCapacity(5)},
			want: limiter.ErrInvalidRate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := limiter.NewInMemory(tt.algo, tt.opts...)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got err %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWithOptions_ValidateOnSet(t *testing.T) {
	_, err := limiter.NewInMemory(
		limiter.AlgorithmFixedWindow,
		limiter.WithLimit(-1),
		limiter.WithWindow(time.Second),
	)
	if !errors.Is(err, limiter.ErrInvalidLimit) {
		t.Fatalf("got err %v, want ErrInvalidLimit", err)
	}
}
