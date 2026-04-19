package TraefikRateLimiter

import (
	"net/http/httptest"
	"testing"
)

func TestCompiledRuleMatches(t *testing.T) {
	rules, err := compileRules([]RuleConfig{
		{Path: "/api/v1/login", MatchType: "exact", Methods: []string{"POST"}, LimitConfig: LimitConfig{Requests: 5, Period: "1m"}},
		{Path: "/api/", MatchType: "prefix", LimitConfig: LimitConfig{Requests: 10, Period: "1s"}},
	})
	if err != nil {
		t.Fatalf("compileRules: %v", err)
	}

	cases := []struct {
		method string
		path   string
		idx    int // expected matching rule index, -1 if none
	}{
		{"POST", "/api/v1/login", 0},
		{"GET", "/api/v1/login", 1}, // method mismatch on first → falls to prefix
		{"GET", "/api/users", 1},
		{"GET", "/other", -1},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		got := -1
		for i, r := range rules {
			if r.matches(req) {
				got = i
				break
			}
		}
		if got != c.idx {
			t.Errorf("%s %s: want rule %d, got %d", c.method, c.path, c.idx, got)
		}
	}
}

func TestExtractIP(t *testing.T) {
	cases := []struct {
		name    string
		strat   IPStrategyConfig
		headers map[string]string
		remote  string
		want    string
	}{
		{
			name:    "default xff leftmost",
			strat:   IPStrategyConfig{Header: "X-Forwarded-For"},
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1, 2.2.2.2, 3.3.3.3"},
			want:    "1.1.1.1",
		},
		{
			name:    "depth 1 rightmost",
			strat:   IPStrategyConfig{Header: "X-Forwarded-For", Depth: 1},
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1, 2.2.2.2, 3.3.3.3"},
			want:    "3.3.3.3",
		},
		{
			name:    "depth 2",
			strat:   IPStrategyConfig{Header: "X-Forwarded-For", Depth: 2},
			headers: map[string]string{"X-Forwarded-For": "1.1.1.1, 2.2.2.2, 3.3.3.3"},
			want:    "2.2.2.2",
		},
		{
			name:    "fallback to trusted header",
			strat:   IPStrategyConfig{Header: "X-Forwarded-For", TrustedHeaders: []string{"X-Real-IP"}},
			headers: map[string]string{"X-Real-IP": "9.9.9.9"},
			want:    "9.9.9.9",
		},
		{
			name:   "fallback to remote addr",
			strat:  IPStrategyConfig{Header: "X-Forwarded-For"},
			remote: "5.5.5.5:1234",
			want:   "5.5.5.5",
		},
		{
			name:    "custom header",
			strat:   IPStrategyConfig{Header: "CF-Connecting-IP"},
			headers: map[string]string{"CF-Connecting-IP": "8.8.8.8"},
			want:    "8.8.8.8",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if c.remote != "" {
				req.RemoteAddr = c.remote
			}
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			if got := extractIP(req, c.strat); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
