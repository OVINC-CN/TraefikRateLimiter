package TraefikRateLimiter

import (
	"context"
	"fmt"
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

func testRedisPrefix(t *testing.T) string {
	return fmt.Sprintf("test_rl:%s:%d:", t.Name(), time.Now().UnixNano())
}

func TestRedisStoreIncrBasic(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	s, err := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: testRedisPrefix(t)})
	if err != nil {
		t.Fatalf("newRedisStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	key := "redis_incr_basic_test_key"
	now := time.Now()

	// First call: count should be 1.
	c1, exp1, err := s.Incr(key, 10*time.Second, now)
	if err != nil {
		t.Fatalf("first Incr err=%v", err)
	}
	if c1 != 1 {
		t.Fatalf("first Incr count=%d want 1", c1)
	}
	if exp1.Before(now) {
		t.Fatalf("expireAt %v is before now %v", exp1, now)
	}

	// Second call: count should be 2, same window.
	c2, _, err := s.Incr(key, 10*time.Second, now)
	if err != nil {
		t.Fatalf("second Incr err=%v", err)
	}
	if c2 != 2 {
		t.Fatalf("second Incr count=%d want 2", c2)
	}
}

func TestRedisStoreKeyPrefix(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	s1, err := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: testRedisPrefix(t) + "a:"})
	if err != nil {
		t.Fatalf("newRedisStore s1: %v", err)
	}
	s2, err := newRedisStore(RedisConfig{Addr: testRedisAddr, KeyPrefix: testRedisPrefix(t) + "b:"})
	if err != nil {
		t.Fatalf("newRedisStore s2: %v", err)
	}
	t.Cleanup(func() { _ = s1.Close() })
	t.Cleanup(func() { _ = s2.Close() })

	now := time.Now()
	key := "shared_key_prefix_test"

	c1, _, err := s1.Incr(key, time.Minute, now)
	if err != nil {
		t.Fatalf("s1 Incr err=%v", err)
	}
	c2, _, err := s2.Incr(key, time.Minute, now)
	if err != nil {
		t.Fatalf("s2 Incr err=%v", err)
	}

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
	cfg.Redis = &RedisConfig{Addr: testRedisAddr, KeyPrefix: testRedisPrefix(t)}

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

func TestMiddlewareWithRedisDefaultAddr(t *testing.T) {
	if !redisAvailable(testRedisAddr) {
		t.Skip("Redis not available at " + testRedisAddr)
	}

	cfg := CreateConfig()
	cfg.Default = LimitConfig{Requests: 1, Period: "1m"}
	cfg.Redis = &RedisConfig{KeyPrefix: testRedisPrefix(t)}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h, err := New(context.Background(), next, cfg, "test-redis-default-addr")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest("GET", "/redis-default-addr", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.101")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d want 200", rr.Code)
	}
}
