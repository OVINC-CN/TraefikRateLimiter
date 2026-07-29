<div align="center">

# TraefikRateLimiter

**URL-level fixed-window rate limiting middleware for Traefik with memory and Redis backends.**

[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · [中文](README_CN.md)

</div>

---

## Overview

TraefikRateLimiter is a [Traefik](https://traefik.io/) middleware plugin that enforces URL-level fixed-window rate limits by path and client IP.
It stores counters in process memory by default, can explicitly use a **Redis counter backend**, and can optionally emit `X-RateLimit-*` headers for access log integration.

## Configuration Constraints (Code-Accurate)

| Field | Required | Notes |
|---|---|---|
| `ipStrategy` | Yes | Object is required; if `header` is empty, defaults to `X-Forwarded-For` |
| `store` | No | `memory` or `redis`, case-insensitive; defaults to `memory` |
| `redis` | Conditional | Required only when `store: redis`; ignored in memory mode |
| `redis.addr` | Conditional | Redis address, e.g. `127.0.0.1:6379` |
| `redis.timeout` | Conditional | Go duration string, e.g. `1s` |
| `default` | Yes | Fallback rate limit for unmatched requests |
| `default.requests` | Yes | Must be `> 0` |
| `default.period` | Yes | Go duration string and must be `>= 1s` |
| `rules[]` | No | Optional list; when present, each rule must be complete |
| `rules[].name` | Yes | Rule identifier, part of rate-limit key |
| `rules[].path` | Yes | Match path |
| `rules[].matchType` | Yes | `exact` or `prefix` |
| `rules[].methods` | No | Optional HTTP method list; if empty, all methods match |
| `rules[].requests` | Yes | Must be `> 0` |
| `rules[].period` | Yes | Go duration string and must be `>= 1s` |
| `addHeaders` | No | Default is `false`; set `true` to write `X-RateLimit-Label` header |
| `addDebugHeaders` | No | Default is `false`; set `true` to write detailed rate-limit debug headers |

> Duration fields are parsed by `time.ParseDuration`. Use values like `1s`, `1m`, `1h`.

> [!IMPORTANT]
> After upgrading, an omitted `store` always selects memory. Existing configurations that contain a `redis` object must add `store: redis` to keep using Redis.

## Installation

### Option A: Local Plugin

Place this repository in Traefik local plugin path:

```text
/plugins-local/src/github.com/OVINC-CN/TraefikRateLimiter/
```

Register it in static Traefik config:

```yaml
experimental:
  localPlugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
```

### Option B: Plugin Catalog

```yaml
experimental:
  plugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
      version: v0.1.0
```

## Dynamic Configuration Examples

### Memory Mode (Default)

```yaml
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          store: memory
          addHeaders: true
          ipStrategy:
            header: "X-Forwarded-For"
            depth: 0
            trustedHeaders:
              - "X-Real-IP"
          default:
            requests: 100
            period: "1m"
          rules:
            - name: "login"
              path: "/api/v1/login"
              matchType: "exact"
              methods: ["POST"]
              requests: 5
              period: "1m"
            - name: "api-prefix"
              path: "/api/"
              matchType: "prefix"
              requests: 60
              period: "10s"
```

The `store` field can be omitted; memory is used when it is not configured.

### Redis Mode

```yaml
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          store: redis
          redis:
            addr: "127.0.0.1:6379"
            password: ""
            db: 0
            keyPrefix: "rl:"
            timeout: "1s"
          ipStrategy:
            header: "X-Forwarded-For"
          default:
            requests: 100
            period: "1m"
```

If Redis initialization or connection fails, middleware initialization fails without falling back to memory.

## Storage Backends

- `memory`: Counters exist only in the current middleware instance in the current Traefik process. They are lost on process restart or configuration reload and are not shared across instances.
- `redis`: Use this backend when counters must be shared across Traefik instances.

## Rate-Limit Behavior

1. Rules are matched in order; the first matched rule is used.
2. If no rule matches, `default` is used.
3. Counter key format: `{ruleName}{ip}{method}#{normalizedPath}{windowIndex}`.

`normalizedPath` reduces key cardinality by normalizing dynamic segments:

- `/orders/123` -> `/orders/:int`
- `/orders/18446744073709551615` -> `/orders/:u64id`
- `/trace/550e8400-e29b-41d4-a716-446655440000` -> `/trace/:uuid`
- `/token/<64-hex>` -> `/token/:hex64`

## Response Headers

Header written when `addHeaders: true`:

| Header | Meaning |
|---|---|
| `X-RateLimit-Label` | HTTP method and normalized path for this request (e.g. `GET#/orders/:int`) |

Headers written when `addDebugHeaders: true`:

| Header | Meaning |
|---|---|
| `X-RateLimit-Used` | Used requests in current window (integer) |
| `X-RateLimit-Remaining` | Remaining requests in current window (integer) |
| `X-RateLimit-RetryAfter` | Seconds until reset (integer) |
| `X-RateLimit-Total` | Window quota for current rule (integer) |
| `X-RateLimit-Period` | Rule period in seconds (integer) |
| `X-RateLimit-Key` | Full counter key used for this request |

When limited, response is `429` with:

- `Retry-After: <seconds>`
- JSON body: `{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 X 秒后重试"}`

When the store fails, response is `500`:

```json
{"error_code":"RATE_LIMIT_STORE_ERROR","error_msg":"限流异常，请稍后重试"}
```

## Access Log Integration (Static Config)

```yaml
accessLog:
  filePath: "/var/log/traefik/access.log"
  format: json
  fields:
    headers:
      defaultMode: drop
      names:
        X-RateLimit-Label: keep
        X-RateLimit-Key: keep
        X-RateLimit-Used: keep
        X-RateLimit-Remaining: keep
        X-RateLimit-RetryAfter: keep
        X-RateLimit-Total: keep
        X-RateLimit-Period: keep
```

## Development

```bash
go vet ./...
go build ./...
go test ./...
```

## License

MIT © [OVINC](https://github.com/OVINC-CN)
