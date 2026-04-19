# TraefikRateLimiter

URL 级别的 Traefik 限流中间件插件,使用进程内内存存储(固定窗口算法),并把限流计数信息写入响应头,便于 Traefik access log 直接采集。

## 特性

- ✅ URL 级别限流(支持 `exact` / `prefix` 路径匹配)
- ✅ HTTP `methods` 条件过滤
- ✅ 限流粒度:`s` / `m` / `h` / `d`(例如 `10s`、`5m`、`2h`、`1d`)
- ✅ 全局 `default` 兜底 + 多条 `rules` 按顺序匹配
- ✅ 自定义 IP 来源(默认 `X-Forwarded-For`,可换 `X-Real-IP`、`CF-Connecting-IP` 等)
- ✅ 支持 `depth`(从右往左数第 N 个)+ `trustedHeaders` 备用列表
- ✅ 把 `Used` / `Remaining` / `RetryAfter` 写到响应头,Traefik access log 一键开启
- ✅ 零第三方依赖,Yaegi 兼容

## 安装

### Local Plugin (推荐)

将本仓库放到 Traefik 的插件目录:`/plugins-local/src/github.com/OVINC/TraefikRateLimiter`,然后在 Traefik 静态配置中声明:

```yaml
experimental:
  localPlugins:
    traefikratelimiter:
      moduleName: github.com/OVINC/TraefikRateLimiter
```

### Plugin Catalog

```yaml
experimental:
  plugins:
    traefikratelimiter:
      moduleName: github.com/OVINC/TraefikRateLimiter
      version: v0.1.0
```

## 配置

### 完整示例(动态配置)

```yaml
http:
  middlewares:
    my-ratelimit:
      plugin:
        traefikratelimiter:
          ipStrategy:
            header: "X-Forwarded-For"      # 可选,默认 X-Forwarded-For
            depth: 0                        # 0 取最左侧;>0 从右往左数第 N 个
            trustedHeaders:                 # 可选,备用候选 header
              - "X-Real-IP"
              - "CF-Connecting-IP"
          default:                          # 可选,所有未匹配规则的请求走这里
            requests: 100
            period: "1m"
          rules:
            - name: "login"                 # 可选,出现在内部 key 中便于排查
              path: "/api/v1/login"
              matchType: "exact"            # exact | prefix,默认 exact
              methods: ["POST"]             # 可选,大小写不敏感
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

### 字段说明

| 字段 | 含义 | 默认 |
| --- | --- | --- |
| `ipStrategy.header` | 主 IP header | `X-Forwarded-For` |
| `ipStrategy.depth` | 0 取最左,>0 从右往左 | `0` |
| `ipStrategy.trustedHeaders` | 备用 header 列表(主 header 命中即用) | `[]` |
| `default.requests` | 默认窗口允许请求数 | 无,可不配 |
| `default.period` | 默认窗口长度 | 无,可不配 |
| `rules[].path` | 规则路径 | 必填 |
| `rules[].matchType` | `exact` / `prefix` | `exact` |
| `rules[].methods` | 限定 HTTP 方法 | 不限 |
| `rules[].requests` | 窗口允许请求数 | 必填,>0 |
| `rules[].period` | 窗口长度,支持 `s`/`m`/`h`/`d` | 必填 |
| `rules[].name` | 规则名,用于内部 key | `r{index}` |

> `default` 与 `rules` 至少配置一个;否则 `New` 会返回错误。

### 限流维度

内部 key 格式:`{ruleID}|{ip}|{realPath}|{windowIndex}`。

这意味着对于一条 `prefix` 规则(例如 `/api/`),`/api/a` 与 `/api/b` 是**两个独立的计数器**——这是有意为之,符合 URL 级别限流的预期。

## Access log 集成

中间件总会写入下面这些响应头:

| Header | 示例 | 说明 |
| --- | --- | --- |
| `X-RateLimit-Limit` | `60` | 当前窗口允许的总请求数 |
| `X-RateLimit-Key` | `login\|10.0.0.1\|/api/v1/login` | 限流维度标识:`{ruleID}\|{ip}\|{realPath}` |
| `X-RateLimit-Used` | `1/1h` | 当前窗口已用请求数 / 周期 |
| `X-RateLimit-Remaining` | `59/1h` | 剩余请求数 / 周期 |
| `X-RateLimit-RetryAfter` | `0s` | 距离窗口重置剩余秒数(带 `s` 单位) |
| `X-RateLimit-Reset` | `1713512345` | 窗口重置的 unix 时间戳 |

被限流时额外返回:

- HTTP 状态:`429 Too Many Requests`
- `Retry-After: <秒数>`(整数,符合 HTTP 语义)

要让这些字段出现在 Traefik 的 access log 中,在 **静态配置** 里启用 `accessLog.fields.headers`:

```yaml
accessLog:
  filePath: "/var/log/traefik/access.log"
  format: json
  fields:
    headers:
      defaultMode: drop
      names:
        X-RateLimit-Used: keep
        X-RateLimit-Remaining: keep
        X-RateLimit-RetryAfter: keep
```

日志中即会出现类似字段:

```json
{
  "...": "...",
  "request_X-RateLimit-Used": "1/1h",
  "request_X-RateLimit-Remaining": "59/1h",
  "request_X-RateLimit-RetryAfter": "0s"
}
```

> Traefik 的 `accessLog.fields.headers` 实际记录的是**响应头**(向客户端返回的),字段名前缀视 Traefik 版本可能为 `request_` 或 `downstream_`,以你环境的实际输出为准。

## 已知局限

- **单进程内存存储**:计数仅在一个 Traefik 实例内有效。多实例部署(滚动升级、HA)时,每个实例独立计数,实际允许的总流量是单实例阈值 × 实例数。如果你需要分布式精确限流,请使用 Traefik 自带的官方限流插件或自行扩展存储后端。
- **固定窗口**:窗口边界处可能出现「翻倍」毛刺(经典固定窗口问题)。对绝对平滑限流敏感的场景建议改用滑动窗口或令牌桶算法。
- **进程重启会清空计数**。

## 开发

```bash
go vet ./...
go build ./...
go test ./...
```

## License

MIT
