package limiter

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/config"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/constant"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/parser"
	"github.com/OVINC-CN/TraefikRateLimiter/internal/store"
)

type RateLimiter struct {
	Name  string
	Next  http.Handler
	Cfg   *config.Config
	Store store.Store
}

func (rl *RateLimiter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// find matching rule
	rule := rl.matchRule(req)
	if rule == nil {
		rl.Next.ServeHTTP(rw, req)
		return
	}

	// copy to avoid concurrent map read/write if rule is modified
	rule = &(*rule)

	// extract client IP
	ip := rl.Cfg.IPStrategy.ExtractIP(req)
	if ip == "" {
		ip = "unknown"
	}

	// parse window
	now := time.Now()
	periodSec := int64(rule.PeriodInner / time.Second)
	if periodSec < 1 {
		periodSec = 1
	}
	window := now.Unix() / periodSec

	// parse path
	method := strings.ToUpper(req.Method)
	rulePath := parser.ParsePath(req.URL.Path)

	// build key
	key := fmt.Sprintf("%s|%s|%s#%s|%d", rule.Name, ip, method, rulePath, window)
	keySimple := fmt.Sprintf("%s#%s", method, rulePath)

	// do incr
	count, expireAt, err := rl.Store.Incr(key, rule.PeriodInner, now)
	if err != nil {
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte(`{"error_code":"RATE_LIMIT_STORE_ERROR","error_msg":"限流异常，请稍后重试"}`))
		return
	}

	// parse remaining and retry after
	remaining := rule.Requests - count
	if remaining < 0 {
		remaining = 0
	}
	retryAfter := int64(expireAt.Sub(now) / time.Second)
	if retryAfter < 0 {
		retryAfter = 0
	}

	// add headers
	if rl.Cfg.AddHeaders {
		rw.Header().Set(constant.HeaderKeySimple, keySimple)
	}
	if rl.Cfg.AddDebugHeaders {
		rw.Header().Set(constant.HeaderUsed, strconv.FormatInt(count, 10))
		rw.Header().Set(constant.HeaderRemaining, strconv.FormatInt(remaining, 10))
		rw.Header().Set(constant.HeaderRetryAfter, strconv.FormatInt(retryAfter, 10))
		rw.Header().Set(constant.HeaderTotal, strconv.FormatInt(rule.Requests, 10))
		rw.Header().Set(constant.HeaderPeriod, strconv.FormatInt(int64(rule.PeriodInner.Seconds()), 10))
		rw.Header().Set(constant.HeaderKey, key)
	}

	// check if over limit
	if count > rule.Requests {
		rw.Header().Set(constant.HeaderRetryAfter, strconv.FormatInt(retryAfter, 10))
		rw.Header().Set("Content-Type", "application/json; charset=utf-8")
		rw.WriteHeader(http.StatusTooManyRequests)
		body := fmt.Sprintf(`{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 %d 秒后重试"}`, retryAfter)
		_, _ = rw.Write([]byte(body))
		return
	}

	rl.Next.ServeHTTP(rw, req)
}

func (rl *RateLimiter) matchRule(req *http.Request) *config.RuleConfig {
	for _, r := range rl.Cfg.Rules {
		if r.Matches(req) {
			return r
		}
	}
	return rl.Cfg.DefaultInner
}
