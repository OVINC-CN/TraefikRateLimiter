// Package traefikratelimiter provides a URL-level, in-memory rate limiting
// middleware for Traefik with access-log friendly response headers.
package TraefikRateLimiter

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MatchType describes how a rule path is compared against the request URL.
type MatchType string

const (
	MatchExact  MatchType = "exact"
	MatchPrefix MatchType = "prefix"
)

// IPStrategyConfig defines how the client IP is extracted from the request.
type IPStrategyConfig struct {
	// Header is the primary header name to read the client IP from. When
	// empty, "X-Forwarded-For" is used.
	Header string `json:"header,omitempty" yaml:"header,omitempty" toml:"header,omitempty"`
	// Depth controls which entry of a comma separated header value is used.
	// 0 (default) means the left-most entry (original client). A positive
	// value counts from the right (1 = right-most).
	Depth int `json:"depth,omitempty" yaml:"depth,omitempty" toml:"depth,omitempty"`
	// TrustedHeaders is an ordered list of additional headers tried after
	// Header. The first non-empty header wins.
	TrustedHeaders []string `json:"trustedHeaders,omitempty" yaml:"trustedHeaders,omitempty" toml:"trustedHeaders,omitempty"`
}

// LimitConfig is the limit definition shared by the default block and
// individual rules.
type LimitConfig struct {
	// Requests is the maximum number of requests allowed during the window.
	Requests int64 `json:"requests,omitempty" yaml:"requests,omitempty" toml:"requests,omitempty"`
	// Period is the size of the fixed window. Examples: "10s", "1m", "2h", "1d".
	Period string `json:"period,omitempty" yaml:"period,omitempty" toml:"period,omitempty"`
}

// RuleConfig describes a single per-path rule.
type RuleConfig struct {
	Requests int64  `json:"requests,omitempty"  yaml:"requests,omitempty"  toml:"requests,omitempty"`
	Period   string `json:"period,omitempty"    yaml:"period,omitempty"    toml:"period,omitempty"`
	// Name is optional and is included in the internal counter key for
	// readability; defaults to "r{index}".
	Name      string   `json:"name,omitempty"      yaml:"name,omitempty"      toml:"name,omitempty"`
	Path      string   `json:"path,omitempty"      yaml:"path,omitempty"      toml:"path,omitempty"`
	MatchType string   `json:"matchType,omitempty" yaml:"matchType,omitempty" toml:"matchType,omitempty"`
	Methods   []string `json:"methods,omitempty"   yaml:"methods,omitempty"   toml:"methods,omitempty"`
}

// RedisConfig holds connection settings for the optional Redis-backed store.
// When Addr is non-empty, the middleware uses Redis instead of the in-process
// memory store, which allows rate-limit state to be shared across multiple
// Traefik replicas.
type RedisConfig struct {
	// Addr is the Redis server address in "host:port" form (default "127.0.0.1:6379").
	Addr string `json:"addr,omitempty" yaml:"addr,omitempty" toml:"addr,omitempty"`
	// Password is sent via AUTH. Leave empty when Redis has no password.
	Password string `json:"password,omitempty" yaml:"password,omitempty" toml:"password,omitempty"`
	// DB selects the logical database (default 0).
	DB int `json:"db,omitempty" yaml:"db,omitempty" toml:"db,omitempty"`
	// KeyPrefix is prepended to every Redis key (e.g. "rl:").
	KeyPrefix string `json:"keyPrefix,omitempty" yaml:"keyPrefix,omitempty" toml:"keyPrefix,omitempty"`
}

// Config is the root configuration consumed by the plugin.
type Config struct {
	IPStrategy IPStrategyConfig `json:"ipStrategy,omitempty" yaml:"ipStrategy,omitempty" toml:"ipStrategy,omitempty"`
	Default    LimitConfig      `json:"default,omitempty"    yaml:"default,omitempty"    toml:"default,omitempty"`
	Rules      []RuleConfig     `json:"rules,omitempty"      yaml:"rules,omitempty"      toml:"rules,omitempty"`
	// AddHeaders controls whether rate-limit response headers are written.
	// Defaults to true. Set to false to suppress all X-RateLimit-* headers.
	AddHeaders *bool `json:"addHeaders,omitempty" yaml:"addHeaders,omitempty" toml:"addHeaders,omitempty"`
	// Redis enables the Redis-backed counter store when non-nil.
	// When Addr is empty, "127.0.0.1:6379" is used.
	Redis *RedisConfig `json:"redis,omitempty" yaml:"redis,omitempty" toml:"redis,omitempty"`
}

// CreateConfig returns a Config populated with sensible defaults.
func CreateConfig() *Config {
	return &Config{
		IPStrategy: IPStrategyConfig{
			Header: "X-Forwarded-For",
			Depth:  0,
		},
	}
}

// parsePeriod parses a duration string with units s, m, h or d.
// Examples: "10s", "5m", "2h", "1d". A bare integer is treated as seconds.
func parsePeriod(period string) (time.Duration, error) {
	period = strings.TrimSpace(period)
	if period == "" {
		return 0, fmt.Errorf("period is empty")
	}
	if n, err := strconv.ParseInt(period, 10, 64); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("period must be > 0")
		}
		return time.Duration(n) * time.Second, nil
	}
	unit := period[len(period)-1]
	value := period[:len(period)-1]
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid period %q: %v", period, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("period must be > 0")
	}
	switch unit {
	case 's':
		return time.Duration(n) * time.Second, nil
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid period unit %q (allowed: s, m, h, d)", string(unit))
	}
}
