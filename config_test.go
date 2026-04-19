package traefikratelimiter

import (
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"10s", 10 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30", 30 * time.Second, false},
		{"  1h  ", time.Hour, false},
		{"", 0, true},
		{"0s", 0, true},
		{"-1m", 0, true},
		{"10x", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parsePeriod(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parsePeriod(%q) expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePeriod(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parsePeriod(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCompileRulesValidation(t *testing.T) {
	cases := []struct {
		name    string
		rules   []RuleConfig
		wantErr bool
	}{
		{"empty path", []RuleConfig{{LimitConfig: LimitConfig{Requests: 1, Period: "1s"}}}, true},
		{"zero requests", []RuleConfig{{Path: "/a", LimitConfig: LimitConfig{Requests: 0, Period: "1s"}}}, true},
		{"bad period", []RuleConfig{{Path: "/a", LimitConfig: LimitConfig{Requests: 1, Period: "1x"}}}, true},
		{"bad matchType", []RuleConfig{{Path: "/a", MatchType: "regex", LimitConfig: LimitConfig{Requests: 1, Period: "1s"}}}, true},
		{"ok exact", []RuleConfig{{Path: "/a", MatchType: "exact", LimitConfig: LimitConfig{Requests: 1, Period: "1s"}}}, false},
		{"ok prefix default matchType", []RuleConfig{{Path: "/a", LimitConfig: LimitConfig{Requests: 1, Period: "1s"}}}, false},
	}
	for _, c := range cases {
		_, err := compileRules(c.rules)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: wantErr=%v err=%v", c.name, c.wantErr, err)
		}
	}
}
