package limiter

import "time"

// Option configures limiter construction.
type Option func(*Config) error

func defaultConfig() Config {
	return Config{Clock: RealClock{}}
}

func applyOptions(opts []Option) (Config, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// WithLimit sets the maximum number of units per window.
func WithLimit(n int) Option {
	return func(c *Config) error {
		if err := validateLimit(n); err != nil {
			return err
		}
		c.Limit = n
		return nil
	}
}

// WithWindow sets the window duration for window-based algorithms.
func WithWindow(d time.Duration) Option {
	return func(c *Config) error {
		if err := validateWindow(d); err != nil {
			return err
		}
		c.Window = d
		return nil
	}
}

// WithRate sets the refill or leak rate in units per second.
func WithRate(r float64) Option {
	return func(c *Config) error {
		if err := validateRate(r); err != nil {
			return err
		}
		c.Rate = r
		return nil
	}
}

// WithCapacity sets the bucket capacity.
func WithCapacity(n int) Option {
	return func(c *Config) error {
		if err := validateCapacity(n); err != nil {
			return err
		}
		c.Capacity = n
		return nil
	}
}

// WithClock sets a custom time source. Nil clocks are ignored.
func WithClock(clock Clock) Option {
	return func(c *Config) error {
		if clock != nil {
			c.Clock = clock
		}
		return nil
	}
}
