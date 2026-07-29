package TraefikRateLimiter

import (
	"context"
	"fmt"
	"net/http"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/config"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/constant"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/limiter"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/store"
)

func CreateConfig() *config.Config {
	return &config.Config{Store: constant.StoreMemory}
}

func New(ctx context.Context, next http.Handler, cfg *config.Config, name string) (http.Handler, error) {
	// validate config
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// init store
	var backend store.Store
	switch cfg.Store {
	case constant.StoreMemory:
		backend = store.NewMemoryStore()
	case constant.StoreRedis:
		rs, err := store.NewRedisStore(cfg.Redis)
		if err != nil {
			return nil, fmt.Errorf("redis store: %w", err)
		}
		backend = rs
	default:
		return nil, fmt.Errorf("unsupported store %q", cfg.Store)
	}

	go func() {
		<-ctx.Done()
		backend.Close()
	}()
	// instance
	return &limiter.RateLimiter{Name: name, Next: next, Cfg: cfg, Store: backend}, nil
}
