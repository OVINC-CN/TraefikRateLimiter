<div align="center">

# TraefikRateLimiter

**Traefik URL 级别限流中间件，基于固定窗口计数器（默认内存，可选 Redis）。**

[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · [中文](README_CN.md)

</div>

---

## 简介

TraefikRateLimiter 是一个 [Traefik](https://traefik.io/) 中间件插件，使用固定窗口算法实现 URL 级别限流。支持路径精确/前缀匹配、HTTP 方法过滤、自定义 IP 提取策略，并通过响应头暴露限流信息，方便 Traefik access log 直接采集。计数后端支持进程内内存（默认）和 Redis（可选）。

## 特性

- 🚦 **URL 级别限流** — 支持精确和前缀路径匹配
- 🔧 **方法过滤** — 仅对指定 HTTP 方法生效
- ⏱️ **灵活时间窗口** — 支持 `s`、`m`、`h`、`d`（如 `10s`、`5m`、`2h`、`1d`）
- 🌐 **可配置 IP 来源** — 自定义 Header、depth 及备用 Header
- 📊 **Access log 友好** — 通过响应头暴露 `Used`、`Remaining`、`RetryAfter`、`Key`
- 🔋 **零外部依赖** — 仅标准库，完全兼容 Yaegi
- 🧩 **可选 Redis 后端** — 多副本共享计数，支持分布式限流
- 🗂️ **默认规则 + 多条路由规则** — 全局兜底与细粒度覆盖

## 安装

### 方式一 — Local Plugin（推荐）

将本仓库放到 Traefik 插件目录：

```
/plugins-local/src/github.com/OVINC-CN/TraefikRateLimiter/
```

在 **静态配置** 中注册：

```yaml
# traefik.yml（静态配置）
experimental:
  localPlugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
```

### 方式二 — Plugin Catalog

```yaml
# traefik.yml（静态配置）
experimental:
  plugins:
    traefikratelimiter:
      moduleName: github.com/OVINC-CN/TraefikRateLimiter
      version: v0.1.0
```

## 配置

### 完整示例

```yaml
# 动态配置
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          ipStrategy:
            header: "X-Forwarded-For"   # 主 IP Header（默认）
            depth: 0                    # 0 = 最左侧；>0 = 从右往左第 N 个
            trustedHeaders:             # 备用 Header，按顺序首个命中即用
              - "X-Real-IP"
          default:                      # 无规则命中时的兜底配置
            requests: 100
            period: "1m"
          redis:                        # 可选：配置后启用 Redis 后端
            addr: "127.0.0.1:6379"      # 可选，默认 127.0.0.1:6379
            password: ""                # 可选
            db: 0                       # 可选，默认 0
            keyPrefix: "rl:"            # 可选
          rules:
            - name: "login"             # 可选，出现在 X-RateLimit-Key 中
              path: "/api/v1/login"
              matchType: "exact"        # exact | prefix（默认 exact）
              methods: ["POST"]         # 不填则匹配所有方法
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

### 配置字段说明

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `ipStrategy.header` | string | `X-Forwarded-For` | 读取客户端 IP 的主 Header |
| `ipStrategy.depth` | int | `0` | `0` = 最左侧；`>0` = 从右往左第 N 个 |
| `ipStrategy.trustedHeaders` | []string | `[]` | 主 Header 无值时依次尝试的备用 Header |
| `default.requests` | int | — | 未命中规则时每窗口最大请求数 |
| `default.period` | string | — | 未命中规则时的窗口长度 |
| `rules[].name` | string | `r{index}` | 可选标识（出现在 `X-RateLimit-Key`） |
| `rules[].path` | string | **必填** | 匹配路径 |
| `rules[].matchType` | string | `exact` | `exact` 或 `prefix` |
| `rules[].methods` | []string | 全部 | 限定 HTTP 方法 |
| `rules[].requests` | int | **必填** | 每窗口最大请求数 |
| `rules[].period` | string | **必填** | 窗口长度（`s` / `m` / `h` / `d`） |
| `addHeaders` | bool | `true` | 是否将 `X-RateLimit-*` 写入响应头 |
| `redis.addr` | string | `127.0.0.1:6379` | Redis 地址（`host:port`） |
| `redis.password` | string | `""` | Redis 密码 |
| `redis.db` | int | `0` | Redis 逻辑库 |
| `redis.keyPrefix` | string | `""` | Redis key 前缀 |

`default` 与 `rules` 至少配置一个，否则 `New` 返回错误。

### Redis 使用说明

- 只要配置了 `redis` 对象即启用 Redis 后端；`redis.addr` 为空时默认 `127.0.0.1:6379`。
- Redis 任意错误都会让当前请求返回 `500`。
- Redis 网络错误时，客户端仍会为后续请求尝试自动重连。
- Redis 计数脚本使用 `EVALSHA`；遇到 `NOSCRIPT` 会自动重新加载脚本并重试一次。
- 建议通过 `redis.keyPrefix` 隔离不同环境或业务的计数 key。

## 限流维度

内部计数器 key 格式：

```
{ruleID} | {ip} | {rulePath} | {windowIndex}
```

`rulePath` 会自动归一化以降低 key 基数，例如：

- `/orders/123` 和 `/orders/456` → `/orders/:int`
- `/trace/550e8400-e29b-41d4-a716-446655440000` → `/trace/:uuid`
- `/token/<64位hex>` → `/token/:hex64`

对于静态路径 `/api/a` 和 `/api/b`，依然保持**独立计数器**。

## 响应头

每个命中规则的请求（无论是否被限流）都会收到以下响应头：

| Header | 示例 | 说明 |
|---|---|---|
| `X-RateLimit-Limit` | `60` | 当前窗口允许的最大请求数 |
| `X-RateLimit-Key` | `default\|10.0.0.1\|/orders/:int` | 限流维度标识 |
| `X-RateLimit-Used` | `1/1h` | 当前窗口已用请求数 / 周期 |
| `X-RateLimit-Remaining` | `59/1h` | 剩余请求数 / 周期 |
| `X-RateLimit-RetryAfter` | `0s` | 距窗口重置剩余秒数（带 `s` 单位） |
| `X-RateLimit-Reset` | `1713512345` | 窗口重置时间（Unix 时间戳） |

### 被限流时的响应（`429`）

```json
HTTP/1.1 429 Too Many Requests
Content-Type: application/json; charset=utf-8
Retry-After: 42
X-RateLimit-Used: 6/1m
X-RateLimit-Remaining: 0/1m
X-RateLimit-RetryAfter: 42s

{"error_code":"RATE_LIMITED","error_msg":"请求过于频繁，请 42 秒后重试"}
```

## Access Log 集成

在 Traefik **静态配置** 中启用响应头采集：

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

日志示例：

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

> 字段名前缀（`request_` 或 `downstream_`）因 Traefik 版本而异，以实际输出为准。

## 已知局限

| 局限 | 说明 |
|---|---|
| **单进程** | 计数器不跨 Traefik 实例共享 |
| **固定窗口毛刺** | 窗口边界可能出现「双倍」流量 |
| **不持久化** | 进程重启后计数器清零 |

## 开发

```bash
go vet ./...
go build ./...
go test ./...
```

## License

MIT © [OVINC](https://github.com/OVINC-CN)
