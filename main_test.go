package TraefikRateLimiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/config"
)

func TestPluginWithMemoryStore(t *testing.T) {
	cfg := CreateConfig()
	if cfg.Store != "memory" {
		t.Fatalf("expected memory store by default, got %q", cfg.Store)
	}

	cfg.AddDebugHeaders = true
	cfg.IPStrategy = &config.IPStrategyConfig{}
	cfg.Default = &config.LimitConfig{
		Requests: 1,
		Period:   "1m",
	}

	nextCalled := false
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		rw.WriteHeader(http.StatusNoContent)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler, err := New(ctx, next, cfg, "test")
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/resource/123", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if !nextCalled {
		t.Fatal("expected the next handler to be called")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if got := recorder.Header().Get("X-RateLimit-Used"); got != "1" {
		t.Fatalf("expected one used request, got %q", got)
	}
}
