package traefikratelimiter

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type compiledRule struct {
	index       int
	name        string
	path        string
	matchType   MatchType
	methods     map[string]struct{}
	requests    int64
	period      time.Duration
	periodLabel string // original config string, used in access-log values.
}

func (r *compiledRule) matches(req *http.Request) bool {
	if len(r.methods) > 0 {
		if _, ok := r.methods[strings.ToUpper(req.Method)]; !ok {
			return false
		}
	}
	switch r.matchType {
	case MatchExact:
		return req.URL.Path == r.path
	case MatchPrefix:
		return strings.HasPrefix(req.URL.Path, r.path)
	}
	return false
}

func (r *compiledRule) id() string {
	if r.name != "" {
		return r.name
	}
	return fmt.Sprintf("r%d", r.index)
}

func compileRules(rules []RuleConfig) ([]*compiledRule, error) {
	out := make([]*compiledRule, 0, len(rules))
	for i, rc := range rules {
		if rc.Path == "" {
			return nil, fmt.Errorf("rules[%d]: path is required", i)
		}
		if rc.Requests <= 0 {
			return nil, fmt.Errorf("rules[%d]: requests must be > 0", i)
		}
		period, err := parsePeriod(rc.Period)
		if err != nil {
			return nil, fmt.Errorf("rules[%d]: %v", i, err)
		}
		mt := MatchType(strings.ToLower(strings.TrimSpace(rc.MatchType)))
		if mt == "" {
			mt = MatchExact
		}
		if mt != MatchExact && mt != MatchPrefix {
			return nil, fmt.Errorf("rules[%d]: invalid matchType %q", i, rc.MatchType)
		}
		methods := map[string]struct{}{}
		for _, m := range rc.Methods {
			m = strings.ToUpper(strings.TrimSpace(m))
			if m != "" {
				methods[m] = struct{}{}
			}
		}
		out = append(out, &compiledRule{
			index:       i,
			name:        rc.Name,
			path:        rc.Path,
			matchType:   mt,
			methods:     methods,
			requests:    rc.Requests,
			period:      period,
			periodLabel: strings.TrimSpace(rc.Period),
		})
	}
	return out, nil
}

// extractIP returns the client IP based on the configured strategy.
func extractIP(req *http.Request, strat IPStrategyConfig) string {
	headers := []string{}
	if strat.Header != "" {
		headers = append(headers, strat.Header)
	}
	for _, h := range strat.TrustedHeaders {
		if h != "" && !equalsAny(h, headers) {
			headers = append(headers, h)
		}
	}
	if len(headers) == 0 {
		headers = []string{"X-Forwarded-For"}
	}
	for _, h := range headers {
		raw := req.Header.Get(h)
		if raw == "" {
			continue
		}
		parts := splitAndTrim(raw, ",")
		if len(parts) == 0 {
			continue
		}
		idx := strat.Depth
		if idx <= 0 {
			return parts[0]
		}
		if idx > len(parts) {
			idx = len(parts)
		}
		return parts[len(parts)-idx]
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func equalsAny(s string, list []string) bool {
	for _, v := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

func splitAndTrim(s, sep string) []string {
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
