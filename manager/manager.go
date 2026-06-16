package manager

import (
	"errors"
	"sync"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

var (
	ErrEmptyKey        = errors.New("manager: key must not be empty")
	ErrNilNewLimiter   = errors.New("manager: NewLimiter must not be nil")
	ErrMaxKeysExceeded = errors.New("manager: max keys exceeded")
)

type Config struct {
	// NewLimiter creates a limiter for the given key.
	NewLimiter func(key string) (limiter.Limiter, error)

	MaxKeys int
}

type Manager struct {
	limiters   sync.Map
	mu         sync.Mutex
	count      int
	maxKeys    int
	newLimiter func(key string) (limiter.Limiter, error)
}

// New creates a Manager.
func New(cfg Config) (*Manager, error) {
	if cfg.NewLimiter == nil {
		return nil, ErrNilNewLimiter
	}
	if cfg.MaxKeys < 0 {
		cfg.MaxKeys = 0
	}
	return &Manager{
		newLimiter: cfg.NewLimiter,
		maxKeys:    cfg.MaxKeys,
	}, nil
}

// Get returns the limiter for key, creating it on first access.
func (m *Manager) Get(key string) (limiter.Limiter, error) {
	if key == "" {
		return nil, ErrEmptyKey
	}

	if v, ok := m.limiters.Load(key); ok {
		return v.(limiter.Limiter), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if v, ok := m.limiters.Load(key); ok {
		return v.(limiter.Limiter), nil
	}

	if m.maxKeys > 0 && m.count >= m.maxKeys {
		return nil, ErrMaxKeysExceeded
	}

	l, err := m.newLimiter(key)
	if err != nil {
		return nil, err
	}

	m.limiters.Store(key, l)
	m.count++
	return l, nil
}

// Delete removes the limiter for key.
func (m *Manager) Delete(key string) {
	if _, loaded := m.limiters.LoadAndDelete(key); !loaded {
		return
	}
	m.mu.Lock()
	m.count--
	m.mu.Unlock()
}

// Len returns the number of cached limiters.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count
}
