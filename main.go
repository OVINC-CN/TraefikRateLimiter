package TraefikRateLimiter

import (
	"context"
	"fmt"
	"net/http"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/config"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/limiter"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/store"
)

func CreateConfig() *config.Config {
	return &config.Config{}
}

func New(ctx context.Context, next http.Handler, cfg *config.Config, name string) (http.Handler, error) {
	// validate config
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// init redis store
	rs, err := store.NewRedisStore(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("redis store: %w", err)
	}
	go func() {
		<-ctx.Done()
		rs.Close()
	}()
	// instance
	return &limiter.RateLimiter{Name: name, Next: next, Cfg: cfg, Store: rs}, nil
}
