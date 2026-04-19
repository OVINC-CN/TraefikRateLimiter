package traefikratelimiter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemStoreFixedWindow(t *testing.T) {
	s := newMemStore()
	now := time.Unix(1000, 0)

	c1, exp1 := s.Incr("k", 10*time.Second, now)
	if c1 != 1 || exp1.Unix() != 1010 {
		t.Fatalf("first incr: count=%d exp=%d", c1, exp1.Unix())
	}
	c2, exp2 := s.Incr("k", 10*time.Second, now.Add(time.Second))
	if c2 != 2 || exp2.Unix() != 1010 {
		t.Fatalf("second incr: count=%d exp=%d", c2, exp2.Unix())
	}

	// after expiry → fresh window
	c3, exp3 := s.Incr("k", 10*time.Second, time.Unix(1011, 0))
	if c3 != 1 || exp3.Unix() != 1021 {
		t.Fatalf("after expiry: count=%d exp=%d", c3, exp3.Unix())
	}
}

func TestMemStoreGC(t *testing.T) {
	s := newMemStore()
	s.Incr("a", time.Second, time.Unix(1000, 0))
	s.Incr("b", 10*time.Second, time.Unix(1000, 0))
	if s.size() != 2 {
		t.Fatalf("size=%d", s.size())
	}
	s.gc(time.Unix(1005, 0))
	if s.size() != 1 {
		t.Fatalf("after gc size=%d", s.size())
	}
}

func TestMemStoreConcurrent(t *testing.T) {
	s := newMemStore()
	now := time.Unix(2000, 0)
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s.Incr("shared", time.Minute, now)
		}()
	}
	wg.Wait()
	c, _ := s.Incr("shared", time.Minute, now)
	if c != int64(n+1) {
		t.Fatalf("expected count=%d, got %d", n+1, c)
	}
}

func TestMiddlewareEnforcesLimit(t *testing.T) {
	cfg := CreateConfig()
	cfg.Default = LimitConfig{Requests: 2, Period: "1h"}

	var nextHits int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHits++
		w.WriteHeader(http.StatusOK)
	})

	h, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	doReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/foo", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	r1 := doReq()
	if r1.Code != http.StatusOK {
		t.Fatalf("r1 code=%d", r1.Code)
	}
	if got := r1.Header().Get(HeaderUsed); got != "1/1h" {
		t.Errorf("r1 Used=%q", got)
	}
	if got := r1.Header().Get(HeaderRemaining); got != "1/1h" {
		t.Errorf("r1 Remaining=%q", got)
	}
	if got := r1.Header().Get(HeaderRetryAfter); !strings.HasSuffix(got, "s") {
		t.Errorf("r1 RetryAfter=%q (should end with s)", got)
	}

	r2 := doReq()
	if r2.Code != http.StatusOK || r2.Header().Get(HeaderRemaining) != "0/1h" {
		t.Errorf("r2 code=%d remaining=%q", r2.Code, r2.Header().Get(HeaderRemaining))
	}

	r3 := doReq()
	if r3.Code != http.StatusTooManyRequests {
		t.Fatalf("r3 expected 429, got %d", r3.Code)
	}
	if r3.Header().Get("Retry-After") == "" {
		t.Errorf("r3 missing Retry-After")
	}
	if ct := r3.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("r3 Content-Type=%q", ct)
	}
	body := r3.Body.String()
	if !strings.Contains(body, `"error_code":"RATE_LIMITED"`) {
		t.Errorf("r3 body missing error_code: %s", body)
	}
	if r3.Header().Get(HeaderUsed) != "3/1h" {
		t.Errorf("r3 Used=%q", r3.Header().Get(HeaderUsed))
	}

	if nextHits != 2 {
		t.Errorf("next handler should be called 2 times, got %d", nextHits)
	}
}

func TestMiddlewareIndependentPathsForPrefixRule(t *testing.T) {
	cfg := CreateConfig()
	cfg.Rules = []RuleConfig{{Path: "/api/", MatchType: "prefix", LimitConfig: LimitConfig{Requests: 1, Period: "1m"}}}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") })
	h, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	send := func(path string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	if c := send("/api/a"); c != http.StatusOK {
		t.Fatalf("/api/a first call code=%d", c)
	}
	if c := send("/api/b"); c != http.StatusOK {
		t.Fatalf("/api/b first call code=%d (should be independent of /api/a)", c)
	}
	if c := send("/api/a"); c != http.StatusTooManyRequests {
		t.Fatalf("/api/a second call should be 429, got %d", c)
	}
}

func TestMiddlewareNoMatchPassesThrough(t *testing.T) {
	cfg := CreateConfig()
	cfg.Rules = []RuleConfig{{Path: "/api/", MatchType: "prefix", LimitConfig: LimitConfig{Requests: 1, Period: "1m"}}}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h, err := New(context.Background(), next, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest("GET", "/other", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !called {
		t.Fatalf("expected next to be called")
	}
	if rr.Header().Get(HeaderUsed) != "" {
		t.Errorf("non-matching request should not have rate limit headers")
	}
}

func TestNewRequiresAnyLimit(t *testing.T) {
	cfg := CreateConfig()
	if _, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), cfg, "test"); err == nil {
		t.Fatal("expected error when no rules and no default")
	}
}
