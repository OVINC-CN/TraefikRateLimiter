<div align="center">

# TraefikRateLimiter

**Traefik URL 级别固定窗口限流中间件（当前实现使用 Redis）。**

[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · [中文](README_CN.md)

</div>

---

## 简介

TraefikRateLimiter 是一个 [Traefik](https://traefik.io/) 中间件插件，按 URL 路径 + 客户端 IP 执行固定窗口限流。
当前代码路径使用 **Redis 计数后端**，并可按需输出 `X-RateLimit-*` 响应头，便于接入 access log。

## 当前配置约束（与代码一致）

| 字段 | 必填 | 说明 |
|---|---|---|
| `ipStrategy` | 是 | 必须提供对象；`header` 为空时默认 `X-Forwarded-For` |
| `redis` | 是 | 必须提供对象；缺失会导致中间件初始化失败 |
| `redis.addr` | 是 | Redis 地址，例如 `127.0.0.1:6379` |
| `redis.timeout` | 是 | Go Duration 字符串，例如 `1s` |
| `default` | 是 | 未命中规则时的兜底限流 |
| `default.requests` | 是 | 必须 `> 0` |
| `default.period` | 是 | Go Duration 字符串，且必须 `>= 1s` |
| `rules[]` | 否 | 可不配置；配置后每条规则都必须完整 |
| `rules[].name` | 是 | 规则标识，参与限流 key 组装 |
| `rules[].path` | 是 | 匹配路径 |
| `rules[].matchType` | 是 | `exact` 或 `prefix` |
| `rules[].requests` | 是 | 必须 `> 0` |
| `rules[].period` | 是 | Go Duration 字符串，且必须 `>= 1s` |
| `addHeaders` | 否 | 默认 `false`，设为 `true` 才会写入限流响应头 |

> 时间字段由 `time.ParseDuration` 解析，建议使用 `1s`、`1m`、`1h`。

## 安装

### 方式一：Local Plugin

将仓库放到 Traefik 本地插件目录：

```text
/plugins-local/src/github.com/OVINC-CN/TraefikRateLimiter/
```

在 Traefik 静态配置中注册：

```yaml
experimental:
  localPlugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
```

### 方式二：Plugin Catalog

```yaml
experimental:
  plugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
      version: v0.1.0
```

## 动态配置示例

```yaml
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          addHeaders: true
          ipStrategy:
            header: "X-Forwarded-For"
            depth: 0
            trustedHeaders:
              - "X-Real-IP"
          redis:
            addr: "127.0.0.1:6379"
            password: ""
            db: 0
            keyPrefix: "rl:"
            timeout: "1s"
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

## 限流行为

1. 按 `rules` 顺序匹配，命中第一条即生效。
2. 未命中任何规则时，使用 `default`。
3. 计数 key 形如：`{ruleName}|{ip}|{normalizedPath}|{windowIndex}`。

`normalizedPath` 会对动态段做归一化，降低 key 基数：

- `/orders/123` -> `/orders/:int`
- `/orders/18446744073709551615` -> `/orders/:u64id`
- `/trace/550e8400-e29b-41d4-a716-446655440000` -> `/trace/:uuid`
- `/token/<64位hex>` -> `/token/:hex64`

## 响应头

仅当 `addHeaders: true` 时写入以下头：

| Header | 含义 |
|---|---|
| `X-RateLimit-Used` | 当前窗口已用次数（整数） |
| `X-RateLimit-Remaining` | 当前窗口剩余次数（整数） |
| `X-RateLimit-RetryAfter` | 距重置剩余秒数（整数） |
| `X-RateLimit-Total` | 当前规则窗口总配额（整数） |
| `X-RateLimit-Period` | 当前规则周期秒数（整数） |
| `X-RateLimit-Key` | 本次请求对应的完整限流 key |

超限时返回 `429`，并包含：

- `Retry-After: <seconds>`
- JSON body: `{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 X 秒后重试"}`

Redis/存储异常时返回 `500`：

```json
{"error_code":"RATE_LIMIT_STORE_ERROR","error_msg":"限流异常，请稍后重试"}
```

## Access Log 集成示例（静态配置）

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
        X-RateLimit-Total: keep
        X-RateLimit-Period: keep
```

## 开发

```bash
go vet ./...
go build ./...
go test ./...
```

## License

MIT © [OVINC](https://github.com/OVINC-CN)
