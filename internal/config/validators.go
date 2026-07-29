package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/OVINC-CN/TraefikRateLimiter/internal/constant"
)

func (c *Config) Validate() error {
	// validate default
	if err := c.validateDefault(); err != nil {
		return err
	}
	// validate rules
	if err := c.validateRules(); err != nil {
		return err
	}
	// validate IP strategy
	if err := c.validateIPStrategy(); err != nil {
		return err
	}
	// validate store config
	if err := c.validateStore(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateDefault() error {
	if c.Default == nil {
		return fmt.Errorf("default config is required")
	}
	var err error
	// requests
	if c.Default.Requests <= 0 {
		return fmt.Errorf("default: requests must be > 0")
	}
	// period
	c.Default.PeriodInner, err = time.ParseDuration(c.Default.Period)
	if err != nil {
		return fmt.Errorf("default: parse period: %v", err)
	}
	if c.Default.PeriodInner < time.Second {
		return fmt.Errorf("default: period must be >= 1s")
	}
	// convert to RuleConfig for default rule
	c.DefaultInner = &RuleConfig{
		Name:        "default",
		Requests:    c.Default.Requests,
		Period:      c.Default.Period,
		PeriodInner: c.Default.PeriodInner,
	}
	return nil
}

func (c *Config) validateRules() error {
	var err error
	for i, r := range c.Rules {
		// name
		if r.Name == "" {
			return fmt.Errorf("rules[%d]: name is required", i)
		}
		// path
		if r.Path == "" {
			return fmt.Errorf("rules[%d]: path is required", i)
		}
		// matchType
		r.MatchType = strings.ToLower(strings.TrimSpace(r.MatchType))
		if r.MatchType == "" {
			return fmt.Errorf("rules[%d]: matchType is required", i)
		}
		if r.MatchType != constant.MatchExact && r.MatchType != constant.MatchPrefix {
			return fmt.Errorf("rules[%d]: invalid matchType %q", i, r.MatchType)
		}
		// requests
		if r.Requests <= 0 {
			return fmt.Errorf("rules[%d]: requests must be > 0", i)
		}
		// period
		r.PeriodInner, err = time.ParseDuration(r.Period)
		if err != nil {
			return fmt.Errorf("rules[%d]: %v", i, err)
		}
		if r.PeriodInner < time.Second {
			return fmt.Errorf("rules[%d]: period must be >= 1s", i)
		}
		// methods
		r.MethodsInner = make(map[string]bool)
		for _, m := range r.Methods {
			m = strings.ToUpper(strings.TrimSpace(m))
			if m == "" {
				continue
			}
			r.MethodsInner[m] = true
		}
	}
	return nil
}

func (c *Config) validateIPStrategy() error {
	if c.IPStrategy == nil {
		return fmt.Errorf("ipStrategy is required")
	}
	if c.IPStrategy.Header == "" {
		c.IPStrategy.Header = "X-Forwarded-For"
	}
	return nil
}

func (c *Config) validateStore() error {
	c.Store = strings.ToLower(strings.TrimSpace(c.Store))
	if c.Store == "" {
		c.Store = constant.StoreMemory
	}

	switch c.Store {
	case constant.StoreMemory:
		return nil
	case constant.StoreRedis:
		return c.validateRedis()
	default:
		return fmt.Errorf("store: invalid value %q", c.Store)
	}
}

func (c *Config) validateRedis() error {
	if c.Redis == nil {
		return fmt.Errorf("redis config is required")
	}
	// addr
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis: addr is required")
	}
	// timeout
	var err error
	c.Redis.TimeoutInner, err = time.ParseDuration(c.Redis.Timeout)
	if err != nil {
		return fmt.Errorf("redis: parse timeout: %v", err)
	}
	return nil
}
