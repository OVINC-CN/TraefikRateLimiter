package TraefikRateLimiter

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// redisAvailable returns true when a Redis server is reachable at addr.
func redisAvailable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

const testRedisAddr = "127.0.0.1:6379"

func TestRedisStoreIncrBasic(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	s, err := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: "test_rl:"})
	if err != nil {
		t.Fatalf("newRedisStore: %v", err)
	}

	key := "redis_incr_basic_test_key"
	now := time.Now()

	// First call: count should be 1.
	c1, exp1 := s.Incr(key, 10*time.Second, now)
	if c1 != 1 {
		t.Fatalf("first Incr count=%d want 1", c1)
	}
	if exp1.Before(now) {
		t.Fatalf("expireAt %v is before now %v", exp1, now)
	}

	// Second call: count should be 2, same window.
	c2, _ := s.Incr(key, 10*time.Second, now)
	if c2 != 2 {
		t.Fatalf("second Incr count=%d want 2", c2)
	}
}

func TestRedisStoreKeyPrefix(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	s1, _ := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: "pfx_a:"})
	s2, _ := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: "pfx_b:"})

	now := time.Now()
	key := "shared_key_prefix_test"

	c1, _ := s1.Incr(key, time.Minute, now)
	c2, _ := s2.Incr(key, time.Minute, now)

	// Different prefixes → independent counters, each starts at 1.
	if c1 != 1 || c2 != 1 {
		t.Fatalf("prefix isolation failed: c1=%d c2=%d", c1, c2)
	}
}

func TestMiddlewareWithRedisStore(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	cfg := CreateConfig()
	cfg.Default = LimitConfig{Requests: 2, Period: "1h"}
	cfg.Redis = &RedisConfig{Addr: testRedisAddr, KeyPrefix: "test_mw:"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, err := New(context.Background(), next, cfg, "test-redis")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	doReq := func() int {
		req := httptest.NewRequest("GET", "/redis-test", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.99")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	if c := doReq(); c != http.StatusOK {
		t.Fatalf("req1 code=%d want 200", c)
	}
	if c := doReq(); c != http.StatusOK {
		t.Fatalf("req2 code=%d want 200", c)
	}
	if c := doReq(); c != http.StatusTooManyRequests {
		t.Fatalf("req3 code=%d want 429", c)
	}
}
