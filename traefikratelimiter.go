package TraefikRateLimiter

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// HeaderUsed exposes the in-window used count, formatted as "<count>/<period>".
const HeaderUsed = "X-RateLimit-Used"

// HeaderRemaining exposes the remaining quota, formatted as "<remaining>/<period>".
const HeaderRemaining = "X-RateLimit-Remaining"

// HeaderRetryAfter exposes the seconds until the window resets, formatted with
// a trailing "s" (e.g. "0s").
const HeaderRetryAfter = "X-RateLimit-RetryAfter"

// HeaderLimit exposes the configured request budget for the window.
const HeaderLimit = "X-RateLimit-Limit"

// HeaderReset exposes the absolute reset time as a unix timestamp.
const HeaderReset = "X-RateLimit-Reset"

// HeaderKey exposes the internal rate-limit key used for this request,
// formatted as "{ruleID}|{ip}|{realPath}". Useful for debugging and access log correlation.
const HeaderKey = "X-RateLimit-Key"

// RateLimiter is the Traefik middleware implementation.
type RateLimiter struct {
	next       http.Handler
	name       string
	cfg        *Config
	rules      []*compiledRule
	def        *compiledRule
	store      *memStore
	addHeaders bool

	now func() time.Time
}

// New is the constructor required by the Traefik plugin loader.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	rules, err := compileRules(config.Rules)
	if err != nil {
		return nil, err
	}

	var def *compiledRule
	if config.Default.Requests > 0 || config.Default.Period != "" {
		if config.Default.Requests <= 0 {
			return nil, fmt.Errorf("default: requests must be > 0")
		}
		period, err := parsePeriod(config.Default.Period)
		if err != nil {
			return nil, fmt.Errorf("default: %v", err)
		}
		def = &compiledRule{
			index:       -1,
			name:        "default",
			matchType:   MatchPrefix,
			path:        "/",
			requests:    config.Default.Requests,
			period:      period,
			periodLabel: config.Default.Period,
		}
	}

	if def == nil && len(rules) == 0 {
		return nil, fmt.Errorf("no default limit and no rules configured")
	}

	store := newMemStore()
	store.startGC(ctx, gcInterval(rules, def))

	addHeaders := true
	if config.AddHeaders != nil {
		addHeaders = *config.AddHeaders
	}

	return &RateLimiter{
		next:       next,
		name:       name,
		cfg:        config,
		rules:      rules,
		def:        def,
		store:      store,
		addHeaders: addHeaders,
		now:        time.Now,
	}, nil
}

func gcInterval(rules []*compiledRule, def *compiledRule) time.Duration {
	duration := time.Duration(0)
	consider := func(d time.Duration) {
		if d <= 0 {
			return
		}
		if duration == 0 || d < duration {
			duration = d
		}
	}
	for _, r := range rules {
		consider(r.period)
	}
	if def != nil {
		consider(def.period)
	}
	if duration == 0 {
		return time.Minute
	}
	half := duration / 2
	if half < time.Second {
		return time.Second
	}
	if half > time.Minute {
		return time.Minute
	}
	return half
}

func (rl *RateLimiter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rule := rl.matchRule(req)
	if rule == nil {
		rl.next.ServeHTTP(rw, req)
		return
	}

	ip := extractIP(req, rl.cfg.IPStrategy)
	if ip == "" {
		ip = "unknown"
	}

	now := rl.now()
	periodSec := int64(rule.period / time.Second)
	if periodSec < 1 {
		periodSec = 1
	}
	window := now.Unix() / periodSec
	key := fmt.Sprintf("%s|%s|%s|%d", rule.id(), ip, req.URL.Path, window)
	// keyLabel is the human-readable portion of the key (without the window index).
	keyLabel := fmt.Sprintf("%s|%s|%s", rule.id(), ip, req.URL.Path)

	count, expireAt := rl.store.Incr(key, rule.period, now)

	remaining := rule.requests - count
	if remaining < 0 {
		remaining = 0
	}
	retryAfter := int64(expireAt.Sub(now) / time.Second)
	if retryAfter < 0 {
		retryAfter = 0
	}

	if rl.addHeaders {
		rw.Header().Set(HeaderLimit, strconv.FormatInt(rule.requests, 10))
		rw.Header().Set(HeaderUsed, fmt.Sprintf("%d/%s", count, rule.periodLabel))
		rw.Header().Set(HeaderRemaining, fmt.Sprintf("%d/%s", remaining, rule.periodLabel))
		rw.Header().Set(HeaderRetryAfter, fmt.Sprintf("%ds", retryAfter))
		rw.Header().Set(HeaderReset, strconv.FormatInt(expireAt.Unix(), 10))
		rw.Header().Set(HeaderKey, keyLabel)
	}

	if count > rule.requests {
		rw.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusTooManyRequests)
		body := fmt.Sprintf(`{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 %d 秒后重试"}`, retryAfter)
		_, _ = rw.Write([]byte(body))
		return
	}

	rl.next.ServeHTTP(rw, req)
}

func (rl *RateLimiter) matchRule(req *http.Request) *compiledRule {
	for _, r := range rl.rules {
		if r.matches(req) {
			return r
		}
	}
	return rl.def
}
