package redis

import (
	"errors"

	"github.com/redis/go-redis/v9"
	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

type buildConfig struct {
	limiter.Config
	client redis.Scripter
	prefix string
}

type Option func(*buildConfig) error

func WithClient(c redis.Scripter) Option {
	return func(cfg *buildConfig) error {
		if c == nil {
			return errors.New("redis: client is nil")
		}
		cfg.client = c
		return nil
	}
}

func WithKeyPrefix(prefix string) Option {
	return func(cfg *buildConfig) error {
		cfg.prefix = prefix
		return nil
	}
}

func WithRate(r float64) Option {
	return func(cfg *buildConfig) error {
		return limiter.WithRate(r)(&cfg.Config)
	}
}

func WithCapacity(n int) Option {
	return func(cfg *buildConfig) error {
		return limiter.WithCapacity(n)(&cfg.Config)
	}
}
