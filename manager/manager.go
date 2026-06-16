package manager

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
)

var (
	ErrEmptyKey        = errors.New("manager: key must not be empty")
	ErrNilNewLimiter   = errors.New("manager: NewLimiter must not be nil")
	ErrMaxKeysExceeded = errors.New("manager: max keys exceeded")
)

// Config configures a Manager.
type Config struct {
	// NewLimiter creates a limiter for the given key.
	NewLimiter func(key string) (limiter.Limiter, error)

	// MaxKeys is an optional hard cap on cached entries. Zero means unlimited.
	MaxKeys int

	// MaxIdle evicts entries not accessed within this duration. Zero disables idle eviction.
	// Call Purge periodically or use RunPurge.
	MaxIdle time.Duration
}

type entry struct {
	lim        limiter.Limiter
	lastAccess atomic.Int64 // Unix nanoseconds
}

func newEntry(lim limiter.Limiter) *entry {
	e := &entry{lim: lim}
	e.touch()
	return e
}

func (e *entry) touch() {
	e.lastAccess.Store(time.Now().UnixNano())
}

// Manager caches in-memory limiters by key.
//
// Call Get on each request so last-access time stays accurate for idle eviction.
// Do not cache limiter.Limiter values across requests without calling Get again.
//
//	lim, err := mgr.Get(userID)
//	result := lim.Allow()
type Manager struct {
	limiters   sync.Map
	mu         sync.Mutex
	count      int
	maxKeys    int
	maxIdle    time.Duration
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
	if cfg.MaxIdle < 0 {
		cfg.MaxIdle = 0
	}
	return &Manager{
		newLimiter: cfg.NewLimiter,
		maxKeys:    cfg.MaxKeys,
		maxIdle:    cfg.MaxIdle,
	}, nil
}

// Get returns the limiter for key, creating it on first access.
func (m *Manager) Get(key string) (limiter.Limiter, error) {
	if key == "" {
		return nil, ErrEmptyKey
	}

	if v, ok := m.limiters.Load(key); ok {
		e := v.(*entry)
		e.touch()
		return e.lim, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if v, ok := m.limiters.Load(key); ok {
		e := v.(*entry)
		e.touch()
		return e.lim, nil
	}

	if m.maxKeys > 0 && m.count >= m.maxKeys {
		return nil, ErrMaxKeysExceeded
	}

	l, err := m.newLimiter(key)
	if err != nil {
		return nil, err
	}

	e := newEntry(l)
	m.limiters.Store(key, e)
	m.count++
	return e.lim, nil
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

// Purge removes entries not accessed within MaxIdle. Returns the number removed.
// No-op when MaxIdle is zero.
func (m *Manager) Purge() int {
	if m.maxIdle <= 0 {
		return 0
	}

	now := time.Now()
	removed := 0

	m.limiters.Range(func(k, v any) bool {
		e := v.(*entry)
		last := e.lastAccess.Load()
		if now.Sub(time.Unix(0, last)) <= m.maxIdle {
			return true
		}
		if !e.lastAccess.CompareAndSwap(last, last) {
			return true // Get touched it between idle check and delete
		}
		if _, loaded := m.limiters.LoadAndDelete(k); !loaded {
			return true
		}
		m.mu.Lock()
		m.count--
		m.mu.Unlock()
		removed++
		return true
	})

	return removed
}

// RunPurge calls Purge every interval until ctx is cancelled.
// When interval is zero, it defaults to 5 minutes.
func (m *Manager) RunPurge(ctx context.Context, interval time.Duration) {
	if m.maxIdle <= 0 {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Purge()
		}
	}
}
