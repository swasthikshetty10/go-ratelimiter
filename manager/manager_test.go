package manager_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
	"github.com/swasthikshetty10/go-ratelimiter/manager"
)

func newTestManager(t *testing.T, maxKeys int, maxIdle time.Duration) *manager.Manager {
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
		MaxIdle: maxIdle,
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
	m := newTestManager(t, 0, 0)

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
	m := newTestManager(t, 0, 0)
	_, err := m.Get("")
	if !errors.Is(err, manager.ErrEmptyKey) {
		t.Fatalf("got err %v, want ErrEmptyKey", err)
	}
}

func TestManager_maxKeys(t *testing.T) {
	m := newTestManager(t, 2, 0)

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
	m := newTestManager(t, 0, 0)
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

func TestManager_Purge_removesIdle(t *testing.T) {
	m := newTestManager(t, 0, 50*time.Millisecond)

	if _, err := m.Get("idle-user"); err != nil {
		t.Fatal(err)
	}
	if m.Len() != 1 {
		t.Fatal("want one entry")
	}

	time.Sleep(60 * time.Millisecond)

	if got := m.Purge(); got != 1 {
		t.Fatalf("Purge() = %d, want 1", got)
	}
	if m.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", m.Len())
	}
}

func TestManager_Purge_keepsActive(t *testing.T) {
	m := newTestManager(t, 0, time.Hour)

	if _, err := m.Get("active"); err != nil {
		t.Fatal(err)
	}
	if got := m.Purge(); got != 0 {
		t.Fatalf("Purge() = %d, want 0", got)
	}
	if m.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", m.Len())
	}
}

func TestManager_Purge_zeroMaxIdle_noop(t *testing.T) {
	m := newTestManager(t, 0, 0)

	if _, err := m.Get("user"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := m.Purge(); got != 0 {
		t.Fatalf("Purge() = %d, want 0 when MaxIdle disabled", got)
	}
}

func TestManager_Get_refreshesIdle(t *testing.T) {
	m := newTestManager(t, 0, 80*time.Millisecond)

	if _, err := m.Get("user"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if _, err := m.Get("user"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if got := m.Purge(); got != 0 {
		t.Fatalf("recent Get should refresh idle: Purge removed %d", got)
	}
}

func TestManager_RunPurge_stopsOnCancel(t *testing.T) {
	m := newTestManager(t, 0, 30*time.Millisecond)

	if _, err := m.Get("user"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.RunPurge(ctx, 20*time.Millisecond)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunPurge did not stop after context cancel")
	}
}
