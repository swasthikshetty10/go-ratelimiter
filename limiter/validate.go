package limiter

import (
	"errors"
	"time"
)

var (
	ErrUnknownBackend   = errors.New("limiter: unknown backend")
	ErrUnknownAlgorithm = errors.New("limiter: unknown algorithm")
	ErrInvalidLimit     = errors.New("limiter: limit must be greater than zero")
	ErrInvalidWindow    = errors.New("limiter: window must be greater than zero")
	ErrInvalidRate      = errors.New("limiter: rate must be greater than zero")
	ErrInvalidCapacity  = errors.New("limiter: capacity must be greater than zero")
)

func validateLimit(limit int) error {
	if limit <= 0 {
		return ErrInvalidLimit
	}
	return nil
}

func validateWindow(window time.Duration) error {
	if window <= 0 {
		return ErrInvalidWindow
	}
	return nil
}

func validateRate(rate float64) error {
	if rate <= 0 {
		return ErrInvalidRate
	}
	return nil
}

func validateCapacity(capacity int) error {
	if capacity <= 0 {
		return ErrInvalidCapacity
	}
	return nil
}

// ValidateLimitAndWindow checks config for window-based algorithms.
func ValidateLimitAndWindow(c Config) error {
	if err := validateLimit(c.Limit); err != nil {
		return err
	}
	return validateWindow(c.Window)
}

// ValidateRateAndCapacity checks config for bucket-based algorithms.
func ValidateRateAndCapacity(c Config) error {
	if err := validateRate(c.Rate); err != nil {
		return err
	}
	return validateCapacity(c.Capacity)
}
