package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/redis"
)

func newTestTokenBucket(t *testing.T, mr *miniredis.Miniredis, rate float64, capacity int) *redis.TokenBucket {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	bucket, err := redis.NewTokenBucket(
		redis.WithClient(client),
		redis.WithKeyPrefix("test:"),
		redis.WithRate(rate),
		redis.WithCapacity(capacity),
	)
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func TestNewTokenBucket_requiresClient(t *testing.T) {
	_, err := redis.NewTokenBucket(
		redis.WithRate(10),
		redis.WithCapacity(5),
	)
	if !errors.Is(err, redis.ErrClientRequired) {
		t.Fatalf("got err %v, want ErrClientRequired", err)
	}
}

func TestNewTokenBucket_validatesRateAndCapacity(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()

	_, err := redis.NewTokenBucket(
		redis.WithClient(client),
		redis.WithCapacity(5),
	)
	if !errors.Is(err, limiter.ErrInvalidRate) {
		t.Fatalf("got err %v, want ErrInvalidRate", err)
	}

	_, err = redis.NewTokenBucket(
		redis.WithClient(client),
		redis.WithRate(10),
	)
	if !errors.Is(err, limiter.ErrInvalidCapacity) {
		t.Fatalf("got err %v, want ErrInvalidCapacity", err)
	}
}

func TestTokenBucket_implementsKeyLimiter(t *testing.T) {
	var _ redis.KeyLimiter = (*redis.TokenBucket)(nil)
}

func TestTokenBucket_startsFull(t *testing.T) {
	mr := miniredis.RunT(t)
	bucket := newTestTokenBucket(t, mr, 10, 5)
	ctx := context.Background()

	got := bucket.AllowNKey(ctx, "user-a", 0)
	if got.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5", got.Remaining)
	}
	if got.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", got.Limit)
	}
}

func TestTokenBucket_initialBurst(t *testing.T) {
	mr := miniredis.RunT(t)
	bucket := newTestTokenBucket(t, mr, 1, 5)
	ctx := context.Background()

	for i := range 5 {
		got := bucket.AllowKey(ctx, "user-a")
		if !got.Allowed {
			t.Fatalf("AllowKey #%d: expected allowed", i+1)
		}
	}

	got := bucket.AllowKey(ctx, "user-a")
	if got.Allowed {
		t.Fatal("6th AllowKey: expected denied")
	}
	if got.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", got.Remaining)
	}
}

func TestTokenBucket_perKeyIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	bucket := newTestTokenBucket(t, mr, 1, 2)
	ctx := context.Background()

	if got := bucket.AllowKey(ctx, "a"); !got.Allowed {
		t.Fatal("a first: want allowed")
	}
	if got := bucket.AllowKey(ctx, "a"); !got.Allowed {
		t.Fatal("a second: want allowed")
	}
	if got := bucket.AllowKey(ctx, "a"); got.Allowed {
		t.Fatal("a third: want denied")
	}

	if got := bucket.AllowKey(ctx, "b"); !got.Allowed {
		t.Fatal("b should have own bucket")
	}
}

func TestTokenBucket_keyPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()

	bucket, err := redis.NewTokenBucket(
		redis.WithClient(client),
		redis.WithKeyPrefix("ratelimit:"),
		redis.WithRate(10),
		redis.WithCapacity(5),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	bucket.AllowKey(ctx, "user-1")

	if !mr.Exists("ratelimit:user-1") {
		t.Fatal("expected Redis key with prefix")
	}
	if mr.Exists("user-1") {
		t.Fatal("key should include prefix")
	}
}

func TestTokenBucket_refillOverTime(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()
	bucket, err := redis.NewTokenBucket(
		redis.WithClient(client),
		redis.WithKeyPrefix("test:"),
		redis.WithRate(10),
		redis.WithCapacity(10),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for bucket.AllowKey(ctx, "user").Allowed {
	}

	got := bucket.AllowKey(ctx, "user")
	if got.Allowed {
		t.Fatal("expected denied immediately after drain")
	}

	now, err := client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	mr.SetTime(now.Add(200 * time.Millisecond))

	got = bucket.AllowKey(ctx, "user")
	if !got.Allowed {
		t.Fatalf("expected allowed after refill, got RetryAfter=%v", got.RetryAfter)
	}
}

func TestTokenBucket_failClosedOnRedisError(t *testing.T) {
	bucket, err := redis.NewTokenBucket(
		redis.WithClient(failScripter{}),
		redis.WithKeyPrefix("test:"),
		redis.WithRate(10),
		redis.WithCapacity(5),
	)
	if err != nil {
		t.Fatal(err)
	}

	got := bucket.AllowKey(context.Background(), "user")
	if got.Allowed {
		t.Fatal("expected fail closed on Redis error")
	}
	if got.Limit != 5 {
		t.Fatalf("Limit = %d, want 5", got.Limit)
	}
}

type failScripter struct{}

func failCmd(ctx context.Context) *goredis.Cmd {
	cmd := goredis.NewCmd(ctx)
	cmd.SetErr(errors.New("redis unavailable"))
	return cmd
}

func (failScripter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *goredis.Cmd {
	return failCmd(ctx)
}

func (failScripter) EvalSha(ctx context.Context, sha1 string, keys []string, args ...interface{}) *goredis.Cmd {
	return failCmd(ctx)
}

func (failScripter) EvalRO(ctx context.Context, script string, keys []string, args ...interface{}) *goredis.Cmd {
	return failCmd(ctx)
}

func (failScripter) EvalShaRO(ctx context.Context, sha1 string, keys []string, args ...interface{}) *goredis.Cmd {
	return failCmd(ctx)
}

func (failScripter) ScriptExists(ctx context.Context, hashes ...string) *goredis.BoolSliceCmd {
	cmd := goredis.NewBoolSliceCmd(ctx)
	cmd.SetErr(errors.New("redis unavailable"))
	return cmd
}

func (failScripter) ScriptLoad(ctx context.Context, script string) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(ctx)
	cmd.SetErr(errors.New("redis unavailable"))
	return cmd
}

func TestTokenBucket_emptyKey(t *testing.T) {
	mr := miniredis.RunT(t)
	bucket := newTestTokenBucket(t, mr, 10, 5)
	ctx := context.Background()

	got := bucket.AllowKey(ctx, "")
	if got.Allowed {
		t.Fatal("empty key: want denied")
	}
}

func TestTokenBucket_setsTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	bucket := newTestTokenBucket(t, mr, 10, 100)
	ctx := context.Background()

	bucket.AllowKey(ctx, "user")

	if ttl := mr.TTL("test:user"); ttl <= 0 {
		t.Fatalf("TTL = %v, want positive", ttl)
	}
}
