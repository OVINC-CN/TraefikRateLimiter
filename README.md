<div align="center">

# TraefikRateLimiter

**URL-level rate limiting middleware for Traefik, backed by fixed-window counters (memory by default, Redis optional).**

[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · [中文](README_CN.md)

</div>

---

## Overview

TraefikRateLimiter is a [Traefik](https://traefik.io/) middleware plugin that enforces URL-level rate limits using a fixed-window algorithm. It supports per-path rules (exact or prefix match), HTTP method filters, a configurable IP extraction strategy, and access-log friendly response headers. The counter backend can be in-memory (default) or Redis (optional).

## Features

- 🚦 **URL-level rate limiting** – exact or prefix path matching
- 🔧 **Method filtering** – apply rules only to specific HTTP methods
- ⏱️ **Flexible time windows** – `s`, `m`, `h`, `d` (e.g. `10s`, `5m`, `2h`, `1d`)
- 🌐 **Configurable IP strategy** – custom header, depth, and fallback headers
- 📊 **Access-log friendly headers** – expose `Used`, `Remaining`, `RetryAfter`, and `Key`
- 🔋 **Zero dependencies** – standard library only, fully Yaegi-compatible
- 🧩 **Optional Redis backend** – shared counters across Traefik replicas
- 🗂️ **Default + per-rule limits** – global fallback with fine-grained overrides

## Installation

### Option A — Local Plugin

Copy (or symlink) this repository into Traefik's local plugin directory:

```
/plugins-local/src/github.com/OVINC-CN/TraefikRateLimiter/
```

Then register it in your **static** Traefik configuration:

```yaml
# traefik.yml (static config)
experimental:
  localPlugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
```

### Option B — Plugin Catalog

```yaml
# traefik.yml (static config)
experimental:
  plugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
      version: v0.1.0
```

## Configuration

### Full Example

```yaml
# dynamic config
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          ipStrategy:
            header: "X-Forwarded-For"   # primary IP header (default)
            depth: 0                    # 0 = left-most; >0 = N-th from right
            trustedHeaders:             # fallback headers, tried in order
              - "X-Real-IP"
          default:                      # catch-all when no rule matches
            requests: 100
            period: "1m"
          redis:                        # optional: enable Redis backend when present
            addr: "127.0.0.1:6379"      # optional, default: 127.0.0.1:6379
            password: ""                # optional
            db: 0                       # optional, default: 0
            keyPrefix: "rl:"            # optional
          rules:
            - name: "login"             # optional; used in X-RateLimit-Key
              path: "/api/v1/login"
              matchType: "exact"        # exact | prefix  (default: exact)
              methods: ["POST"]         # omit to match all methods
              requests: 5
              period: "1m"
            - path: "/api/"
              matchType: "prefix"
              requests: 60
              period: "1s"

  routers:
    api:
      rule: "PathPrefix(`/`)"
      service: my-service
      middlewares:
        - my-ratelimit
```

### Configuration Reference

| Field | Type | Default | Description |
|---|---|---|---|
| `ipStrategy.header` | string | `X-Forwarded-For` | Primary header to read the client IP from |
| `ipStrategy.depth` | int | `0` | `0` = left-most entry; `>0` = N-th from right |
| `ipStrategy.trustedHeaders` | []string | `[]` | Fallback headers tried after the primary one |
| `default.requests` | int | — | Max requests per window for unmatched routes |
| `default.period` | string | — | Window size for the default limit |
| `rules[].name` | string | `r{index}` | Optional label (appears in `X-RateLimit-Key`) |
| `rules[].path` | string | **required** | Path to match |
| `rules[].matchType` | string | `exact` | `exact` or `prefix` |
| `rules[].methods` | []string | all | HTTP methods to apply the rule to |
| `rules[].requests` | int | **required** | Max requests allowed per window |
| `rules[].period` | string | **required** | Window size (`s` / `m` / `h` / `d`) |
| `addHeaders` | bool | `true` | Write `X-RateLimit-*` headers to the response |
| `redis.addr` | string | `127.0.0.1:6379` | Redis server address (`host:port`) |
| `redis.password` | string | `""` | Redis password |
| `redis.db` | int | `0` | Redis logical DB index |
| `redis.keyPrefix` | string | `""` | Prefix prepended to all Redis keys |

At least one of `default` or `rules` must be configured; `New` returns an error otherwise.

### Redis Backend Notes

- Redis backend is enabled when the `redis` block is present. If `redis.addr` is empty, `127.0.0.1:6379` is used.
- On any Redis error, middleware returns `500` for the current request.
- On Redis network errors, the client still attempts reconnect for subsequent requests.
- Use `redis.keyPrefix` to isolate keys between environments/services.

## Rate Limit Dimensions

The internal counter key is:

```
{ruleID} | {ip} | {realPath} | {windowIndex}
```

For a prefix rule on `/api/`, the paths `/api/a` and `/api/b` maintain **separate counters** — this is intentional and matches the expectation of URL-level rate limiting.

## Response Headers

These headers are set on **every matched request**, whether allowed or rejected:

| Header | Example | Description |
|---|---|---|
| `X-RateLimit-Limit` | `60` | Maximum requests configured for the window |
| `X-RateLimit-Key` | `login\|10.0.0.1\|/api/v1/login` | Rate-limit dimension identifier |
| `X-RateLimit-Used` | `1/1h` | Requests used in this window / period |
| `X-RateLimit-Remaining` | `59/1h` | Remaining requests / period |
| `X-RateLimit-RetryAfter` | `0s` | Seconds until the window resets (with `s` suffix) |
| `X-RateLimit-Reset` | `1713512345` | Window reset time as a Unix timestamp |

### Rate-Limited Response (`429`)

```json
HTTP/1.1 429 Too Many Requests
Content-Type: application/json; charset=utf-8
Retry-After: 42
X-RateLimit-Used: 6/1m
X-RateLimit-Remaining: 0/1m
X-RateLimit-RetryAfter: 42s

{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 42 秒后重试"}
```

## Access Log Integration

To surface rate-limit headers in Traefik's access log, add the following to your **static** configuration:

```yaml
accessLog:
  filePath: "/var/log/traefik/access.log"
  format: json
  fields:
    headers:
      defaultMode: drop
      names:
        X-RateLimit-Key: keep
        X-RateLimit-Used: keep
        X-RateLimit-Remaining: keep
        X-RateLimit-RetryAfter: keep
```

Example log entry:

```json
{
  "RequestPath": "/api/v1/login",
  "DownstreamStatus": 429,
  "request_X-RateLimit-Key": "login|10.0.0.1|/api/v1/login",
  "request_X-RateLimit-Used": "6/1m",
  "request_X-RateLimit-Remaining": "0/1m",
  "request_X-RateLimit-RetryAfter": "42s"
}
```

> The exact header field prefix (`request_` vs `downstream_`) depends on your Traefik version and configuration. Check your environment's actual output.

## Known Limitations

| Limitation | Detail |
|---|---|
| **Single-process only** | Counters are in-memory and not shared across Traefik replicas. |
| **Fixed-window bursting** | Classic "double-hit" at window boundaries is possible. |
| **No persistence** | Counters reset on process restart. |

## Development

```bash
# Lint & vet
go vet ./...

# Build
go build ./...

# Test
go test ./...
```

## License

MIT © [OVINC](https://github.com/OVINC-CN)
