package TraefikRateLimiter

import (
	"context"
	"sync"
	"time"
)

// rateStore is the backend interface used by the middleware to count requests.
type rateStore interface {
	Incr(key string, ttl time.Duration, now time.Time) (int64, time.Time)
}

type counter struct {
	count      int64
	expireUnix int64
}

// memStore is a lightweight thread-safe fixed-window counter store.
type memStore struct {
	mu   sync.Mutex
	data map[string]*counter

	stop chan struct{}
}

func newMemStore() *memStore {
	return &memStore{
		data: make(map[string]*counter),
		stop: make(chan struct{}),
	}
}

// Incr increments the counter for key. If the key does not yet exist or has
// expired, a new window is started with TTL=ttl. The current count and the
// absolute expiration time are returned.
func (s *memStore) Incr(key string, ttl time.Duration, now time.Time) (int64, time.Time) {
	nowUnix := now.Unix()
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.data[key]
	if !ok || c.expireUnix <= nowUnix {
		ttlSec := int64(ttl / time.Second)
		if ttlSec < 1 {
			ttlSec = 1
		}
		c = &counter{count: 1, expireUnix: nowUnix + ttlSec}
		s.data[key] = c
		return c.count, time.Unix(c.expireUnix, 0)
	}
	c.count++
	return c.count, time.Unix(c.expireUnix, 0)
}

// gc removes expired entries.
func (s *memStore) gc(now time.Time) {
	nowUnix := now.Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, c := range s.data {
		if c.expireUnix <= nowUnix {
			delete(s.data, k)
		}
	}
}

// startGC launches a background goroutine that periodically purges expired
// entries. The goroutine exits when ctx is cancelled or Stop is called.
func (s *memStore) startGC(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case now := <-t.C:
				s.gc(now)
			}
		}
	}()
}

// Stop signals the background GC goroutine to exit.
func (s *memStore) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// size returns the current number of tracked keys (used in tests).
func (s *memStore) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data)
}
