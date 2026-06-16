package manager_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
	"github.com/swasthikshetty10/go-ratelimiter/manager"
)

func newTestManager(t *testing.T, maxKeys int) *manager.Manager {
	t.Helper()
	m, err := manager.New(manager.Config{
		NewLimiter: func(_ string) (limiter.Limiter, error) {
			return inmemory.New(
				limiter.AlgorithmFixedWindow,
				limiter.WithLimit(2),
				limiter.WithWindow(time.Minute),
			)
		},
		MaxKeys: maxKeys,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func allow(t *testing.T, m *manager.Manager, key string) limiter.Result {
	t.Helper()
	lim, err := m.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	return lim.Allow()
}

func TestManager_Get_perKeyIsolation(t *testing.T) {
	m := newTestManager(t, 0)

	if got := allow(t, m, "user-a"); !got.Allowed {
		t.Fatal("user-a first: want allowed")
	}
	if got := allow(t, m, "user-a"); !got.Allowed {
		t.Fatal("user-a second: want allowed")
	}
	if got := allow(t, m, "user-a"); got.Allowed {
		t.Fatal("user-a third: want denied")
	}

	if got := allow(t, m, "user-b"); !got.Allowed {
		t.Fatal("user-b should have own bucket")
	}
}

func TestManager_emptyKey(t *testing.T) {
	m := newTestManager(t, 0)
	_, err := m.Get("")
	if !errors.Is(err, manager.ErrEmptyKey) {
		t.Fatalf("got err %v, want ErrEmptyKey", err)
	}
}

func TestManager_maxKeys(t *testing.T) {
	m := newTestManager(t, 2)

	if _, err := m.Get("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("c"); !errors.Is(err, manager.ErrMaxKeysExceeded) {
		t.Fatalf("got err %v, want ErrMaxKeysExceeded", err)
	}

	m.Delete("a")
	if _, err := m.Get("c"); err != nil {
		t.Fatalf("after delete, want room for c: %v", err)
	}
}

func TestManager_Concurrent(t *testing.T) {
	m := newTestManager(t, 0)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "user"
			if id%2 == 0 {
				key = "other"
			}
			lim, err := m.Get(key)
			if err != nil {
				return
			}
			lim.Allow()
		}(i)
	}
	wg.Wait()
	if m.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", m.Len())
	}
}

func TestNew_nilNewLimiter(t *testing.T) {
	_, err := manager.New(manager.Config{})
	if !errors.Is(err, manager.ErrNilNewLimiter) {
		t.Fatalf("got err %v, want ErrNilNewLimiter", err)
	}
}
