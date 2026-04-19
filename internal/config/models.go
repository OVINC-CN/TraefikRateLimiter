package config

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/constant"
)

type IPStrategyConfig struct {
	Header         string   `json:"header,omitempty" yaml:"header,omitempty" toml:"header,omitempty"`
	Depth          int      `json:"depth,omitempty" yaml:"depth,omitempty" toml:"depth,omitempty"`
	TrustedHeaders []string `json:"trustedHeaders,omitempty" yaml:"trustedHeaders,omitempty" toml:"trustedHeaders,omitempty"`
}

func (ipCfg *IPStrategyConfig) ExtractIP(req *http.Request) string {
	// parse headers in order: Header, then TrustedHeaders
	headers := make([]string, 0, len(ipCfg.TrustedHeaders)+1)
	if ipCfg.Header != "" {
		headers = append(headers, ipCfg.Header)
	}
	for _, h := range ipCfg.TrustedHeaders {
		if h != "" {
			headers = append(headers, h)
		}
	}
	if len(headers) == 0 {
		headers = []string{"X-Forwarded-For"}
	}
	// extract IP from headers
	for _, h := range headers {
		raw := req.Header.Get(h)
		if raw == "" {
			continue
		}
		parts := ipCfg.splitAndTrim(raw, ",")
		if len(parts) == 0 {
			continue
		}
		idx := ipCfg.Depth
		if idx <= 0 {
			return parts[0]
		}
		if idx > len(parts) {
			idx = len(parts)
		}
		return parts[len(parts)-idx]
	}
	// fallback to RemoteAddr
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func (ipCfg *IPStrategyConfig) splitAndTrim(s, sep string) []string {
	raw := strings.Split(s, sep)
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type LimitConfig struct {
	Requests    int64         `json:"requests,omitempty" yaml:"requests,omitempty" toml:"requests,omitempty"`
	Period      string        `json:"period,omitempty" yaml:"period,omitempty" toml:"period,omitempty"`
	PeriodInner time.Duration `json:"-" yaml:"-" toml:"-"`
}

type RuleConfig struct {
	LimitConfig
	Name         string          `json:"name,omitempty"      yaml:"name,omitempty"      toml:"name,omitempty"`
	Methods      []string        `json:"methods,omitempty"   yaml:"methods,omitempty"   toml:"methods,omitempty"`
	MethodsInner map[string]bool `json:"-" yaml:"-" toml:"-"`
	Path         string          `json:"path,omitempty"      yaml:"path,omitempty"      toml:"path,omitempty"`
	MatchType    string          `json:"matchType,omitempty" yaml:"matchType,omitempty" toml:"matchType,omitempty"`
	Requests     int64           `json:"requests,omitempty"  yaml:"requests,omitempty"  toml:"requests,omitempty"`
	Period       string          `json:"period,omitempty"    yaml:"period,omitempty"    toml:"period,omitempty"`
	PeriodInner  time.Duration   `json:"-" yaml:"-" toml:"-"`
}

func (r *RuleConfig) Matches(req *http.Request) bool {
	if len(r.Methods) > 0 {
		if !r.MethodsInner[strings.ToUpper(req.Method)] {
			return false
		}
	}
	switch r.MatchType {
	case constant.MatchExact:
		return strings.EqualFold(req.URL.Path, r.Path)
	case constant.MatchPrefix:
		return strings.HasPrefix(req.URL.Path, r.Path)
	}
	return false
}

type RedisConfig struct {
	Addr         string        `json:"addr,omitempty" yaml:"addr,omitempty" toml:"addr,omitempty"`
	Password     string        `json:"password,omitempty" yaml:"password,omitempty" toml:"password,omitempty"`
	DB           uint          `json:"db,omitempty" yaml:"db,omitempty" toml:"db,omitempty"`
	KeyPrefix    string        `json:"keyPrefix,omitempty" yaml:"keyPrefix,omitempty" toml:"keyPrefix,omitempty"`
	Timeout      string        `json:"timeout,omitempty" yaml:"timeout,omitempty" toml:"timeout,omitempty"`
	TimeoutInner time.Duration `json:"-" yaml:"-" toml:"-"`
}

type Config struct {
	AddHeaders   bool              `json:"addHeaders,omitempty" yaml:"addHeaders,omitempty" toml:"addHeaders,omitempty"`
	Redis        *RedisConfig      `json:"redis,omitempty" yaml:"redis,omitempty" toml:"redis,omitempty"`
	IPStrategy   *IPStrategyConfig `json:"ipStrategy,omitempty" yaml:"ipStrategy,omitempty" toml:"ipStrategy,omitempty"`
	Default      *LimitConfig      `json:"default,omitempty"    yaml:"default,omitempty"    toml:"default,omitempty"`
	DefaultInner *RuleConfig       `json:"-" yaml:"-" toml:"-"`
	Rules        []*RuleConfig     `json:"rules,omitempty"      yaml:"rules,omitempty"      toml:"rules,omitempty"`
}
