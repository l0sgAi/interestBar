# 限流中间件与邮箱验证码注册安全加固 · 设计文档

> 状态：**设计稿（待评审实施）**
> 作者：losgai
> 日期：2026-06-22
> 关联代码：`pkg/domains/auth/**`、`pkg/server/storage/redis/**`、`pkg/composition/middleware/**`、`pkg/server/router/router.go`、`cmd/apps/server.go`

---

## 1. 背景与目标

### 1.1 背景

当前邮箱验证码注册采用「发送验证码 → 校验验证码 → 完成注册」三步流程（见 `pkg/domains/auth/application/service.go` 的 `SendCode` / `VerifyCode` / `Register`）。经审计，该流程存在三个安全隐患：

| # | 问题 | 位置 | 说明 |
|---|------|------|------|
| ⚠️1 | 验证码爆破无任何防护（最严重） | `VerifyCode` | 失败**不删码、不计数、不限速**。6 位数字 = 100 万种组合，验证码 5 分钟有效窗口，攻击者高频试错即可命中。 |
| ⚠️2 | 验证码用 `math/rand`，非密码学安全 | `service.go:12, 175` | `rand.Intn` 是伪随机；种子可预测时验证码序列可预测。 |
| ⚠️3 | `verified` 标记可复用窗口 | `Register` | TTL 10 分钟、注册成功才删除。一旦问题 1 被爆破通过，10 分钟窗口内即可注册。 |

此外，现有「请求验证码限流」只有**邮箱维度 60 秒一条**这一道（`register:rate:{email}`，TTL 60s），**没有 IP 维度 / 全局维度的限流中间件**，存在邮件轰炸与跨邮箱爆破的可利用缺口。

### 1.2 目标

1. 探索并评估业界成熟的限流中间件方案，判断是否可直接采用。
2. 结合本仓库现状（Hertz + go-redis/v9 + 已有 Lua 脚本范式），设计一套**可复用的 Redis 限流原语 + Hertz 中间件**，沉淀为设计文档供后续实施。
3. 将方案应用到「邮箱验证码注册」场景，解决上述三个问题。
4. 改动需符合现有 DDD 分层与「框架无关路由抽象（`pkg/shared/routing`）」的既有约定，尽量小侵入、可灰度、可回滚。

### 1.3 非目标

- 不重写 OAuth 登录链路。
- 不引入新的 Redis 客户端或新的基础设施（继续复用 `redis.Client` 全局实例与现有 `Set/Get/Del/Exists/Incr` 封装）。
- 不在本期实现「一次性 verify token 绑定」（见 §13 未来演进，列为后续可选项）。

---

## 2. 现状梳理（实现基线）

### 2.1 三步注册流程

```
POST /auth/register/send-code   SendCode:    校验邮箱→查重→60s邮箱限流→生成6位码(math/rand)→写Redis(5min)→发邮件
POST /auth/register/verify      VerifyCode:  读码→比对(失败不删码)→删码→置verified(10min)
POST /auth/register/complete    Register:    校验长度→IsVerified门控→再次查重→建用户→登录→删verified
```

### 2.2 现有限流

- 唯一限流点：`SendCode` 内 `register:rate:{email}`（TTL 60s），仅邮箱维度。
- 错误类型 `errRateLimitExceeded` / `IsRateLimitExceededErr` 只服务于这一处，handler 层映射为 HTTP 429。
- `pkg/server/router/router.go` 全局只挂了 `Logger` 和 `CORS`；**无任何限流中间件**。

### 2.3 已有的 Redis Lua 范式（关键复用点）

仓库已在 `pkg/server/storage/redis/` 沉淀了成熟的 Lua 脚本范式，**这是本设计直接复用的基础设施**：

| 文件 | 模式 |
|------|------|
| `like_lua.go` | `ScriptLoad` 预加载 → `EvalSha` 执行 → 命中 `NOSCRIPT` 则重新 `ScriptLoad` 并重试 |
| `view_lua.go` | 同上（浏览量去重递增） |
| `cmd/apps/server.go:90-97` | 启动时依次调用 `InitLikeLuaScripts()` / `InitViewLuaScripts()` |

启动装配链：`InitRedis` → `InitXxxLuaScripts` → `InitRouter`（见 `cmd/apps/server.go`）。

### 2.4 框架与分层约束

- HTTP 框架：**CloudWeGo Hertz**（`github.com/cloudwego/hertz v0.10.5`），中间件签名 `func(ctx context.Context, c *app.RequestContext)`。
- 框架无关路由抽象：`pkg/shared/routing`，`routing.HandlerFunc = func(c appctx.AppContext)`。领域路由（`domains/*/interfaces/http/routes.go`）只依赖此抽象，不 import hertz。
- `AppContext`（`pkg/shared/appctx/context.go`）**当前未暴露 `ClientIP()`**；`Logger`/`CORS` 等 hertz 原生中间件直接用 `c.ClientIP()`。
- 鉴权中间件 `RequireLogin` 是 `routing.HandlerFunc`，由 composition 层注入到 `RegisterRoutes`（见 `pkg/composition/server.go` 的 `registerAuth`）。

### 2.5 一个顺带发现的问题（需在实施时一并修正）

`pkg/server/storage/redis/cache.go:117-128` 的 `Incr` 封装**每次调用都执行 `Expire`**（滑动 TTL），对「窗口计数」语义是错的——窗口永不到期。正确的固定窗口应是 `INCR; if count==1 then EXPIRE end`。本期新增的限流原语会采用正确范式；旧 `Incr` 是否回改不在本期范围（仅记录）。

---

## 3. 业界方案调研结论

> 调研覆盖 ulule/limiter、go-redis/redis_rate、didip/tollbooth、gin-contrib/limits、throttled、golang.org/x/time/rate、redis-cell，以及限流算法与 Redis 原子性。要点如下（详细来源见 §15）。

### 3.1 候选库评估

| 库 | 算法 | Redis | Hertz 适配 | 维护状态 | 结论 |
|----|------|-------|-----------|----------|------|
| `ulule/limiter/v3` | 固定窗口 | ✅ Lua | ❌ 仅 gin/fasthttp/stdlib | v3.11.2（2023-05），3 年无新版 | 不采用：固定窗口有边界突刺，且无 Hertz 适配 |
| `go-redis/redis_rate/v10` | GCRA | ✅ Lua | ❌ 需自写 ~20 行 | v10.0.1（2023-04），维护停滞但未归档 | 备选：仅适合「流量整形」，不适合「硬上限计数」 |
| `didip/tollbooth` | 令牌桶 | ❌ 仅内存 | — | 活跃 | 淘汰：不支持分布式 |
| `gin-contrib/limits` | 多为内存 | ❌ | — | 稀疏 | 淘汰：不支持分布式 |
| `throttled/v2` | GCRA | ✅ | 需适配 | 低频 | 备选，但同 GCRA 局限 |
| `golang.org/x/time/rate` | 令牌桶 | ❌ 仅内存 | — | 官方 | 淘汰：单实例，多副本失效 |
| `redis-cell`（`CL.THROTTLE`） | GCRA | 模块 | — | — | 淘汰：托管 Redis（AWS ElastiCache / GCP Memorystore / Azure）普遍不支持 MODULE LOAD，可移植性差 |

**结论：没有「开箱即用、适配 Hertz、同时满足整形与硬上限」的成熟中间件。**

### 3.2 算法对比（决定方案形态）

| 算法 | Redis 开销 | 平滑度 | 硬上限？ | 容忍突发？ | 适用场景 |
|------|-----------|--------|---------|-----------|----------|
| 固定窗口 | O(1) | 差（边界 2× 突刺） | ✅ | ❌ | 不推荐用于公开端点 |
| **滑动窗口计数（加权双桶）** | O(1) | 良好 | 近似（±半窗） | ✅ | **本方案采用：IP 整形** |
| 滑动窗口日志（ZSET） | O(N) | 极佳 | ✅ | ✅ | 内存/CPU 偏高，暂不需要 |
| 令牌桶 / GCRA | O(1) | 良好 | ❌（突发） | ✅ | 适合整形，不适合「5 次硬上限」 |

关键差异：**「每分钟 N 次请求」的流量整形**（IP）与**「累计 N 次失败即锁定」的硬上限计数**（验证码尝试）是两类语义，不能用同一种 GCRA 既表达「突发容忍」又表达「硬上限」。

### 3.3 原子性要点

- `GET → 判断 → INCR` 是经典 check-then-act 竞态，并发下会放行超额请求。
- `INCR` 后**无条件** `EXPIRE` 会让窗口永不到期（见 §2.5）。
- 正确做法：**用 Lua 脚本**把「读计数 + 判断 + 写计数 + 仅首次设 TTL」封装成原子单元；或 `INCR; if count==1 then EXPIRE end`。
- 加载范式：启动 `SCRIPT LOAD` 缓存 SHA → 运行时 `EVALSHA` → 命中 `NOSCRIPT` 重新加载并重试（**仓库已在 `like_lua.go`/`view_lua.go` 正确实现**）。

---

## 4. 选型决策

### 4.1 决策：自研「滑动窗口计数 + 硬上限计数」双原语，统一用 Lua

**采用自研，不引入第三方限流库。** 理由：

1. **基础设施已就位**：`like_lua.go` / `view_lua.go` 已是可复制的 `ScriptLoad + EvalSha + reload-on-NOSCRIPT` 范式，限流原语只是再加一个 Lua 字符串 + Go 包装，**零新依赖**。
2. **一个机制覆盖两类语义**：滑动窗口计数脚本（配 `limit/window`）服务 IP 整形；同一套「固定窗口计数 + 首次设 TTL」思路服务验证码尝试硬上限。`redis_rate`（GCRA）无法表达硬上限，最终还是要再写一套硬上限逻辑——反而两套机制。
3. **安全路径自控**：防爆破是安全敏感路径，自控 ~120 行 Lua 比依赖一个 3 年未更新、go-redis 版本钉死在 `v9.0.2`（仓库为 `v9.19.0`）的库更易审计。
4. **Hertz 中间件成本相同**：无论调 `redis_rate.Allow` 还是自研 `limiter.Allow`，Hertz 包装层都是 ~20 行（取 key → 判定 → 429/放行）。库并不省这层工作。
5. **可移植**：纯 `EVAL`/`EVALSHA`，不依赖 `redis-cell` 模块，兼容所有托管 Redis。

### 4.2 何时改用 `redis_rate`（备选记录）

若未来需要：GCRA 的自然突发整形（交互体验），或大量分层限流维度（路由/套餐/API Key）。届时 `redis_rate` 接入成本约 30 分钟，可平滑替换 IP 整形部分（验证码硬上限仍保留自研）。

### 4.3 防御层次总览

针对三个问题，采用「纵深防御」，每层独立可回滚：

```
┌─────────────────────────────────────────────────────────────────┐
│  L0  验证码强度        crypto/rand 生成 6 位（修问题 2）          │
├─────────────────────────────────────────────────────────────────┤
│  L1  IP 流量整形        滑动窗口计数中间件，覆盖                  │
│                        /send-code /verify /complete /login       │
│                        （防邮件轰炸、跨邮箱爆破、密码爆破）        │
├─────────────────────────────────────────────────────────────────┤
│  L2  验证码尝试硬上限   单邮箱失败计数 + 锁定（修问题 1）         │
│                        原子 verify-attempt Lua                   │
├─────────────────────────────────────────────────────────────────┤
│  L3  verified 窗口收紧  TTL 10min→5min；依赖 L2 使爆破不可行      │
│                        （修问题 3）                               │
└─────────────────────────────────────────────────────────────────┘
```

- 问题 1：主要靠 **L2**（5 次失败锁定，验证码 5 分钟内最多试 5 次 → 爆破成功率 ≈ 5×10⁻⁶），L1 提供横向兜底。
- 问题 2：靠 **L0**。
- 问题 3：主要靠 **L2** 使爆破路径不可达（没有 verified 标记就注册不了，而爆破拿不到 verified），L3 收紧窗口为纵深加固。

---

## 5. 总体设计

### 5.1 限流原语分层

```
pkg/server/storage/redis/
  ├─ ratelimit_lua.go   [新增] 滑动窗口计数脚本 + InitRateLimitLuaScripts + SlidingWindowAllow
  ├─ verify.go          [扩展] 复用现有验证码 key；新增尝试/锁定 key 与常量
  ├─ verify_lua.go      [新增] 原子 verify-attempt 脚本 + Init + AtomicVerifyAttempt
  └─ constants.go       [扩展] 新增 register:attempts / register:lockout 前缀与 getter

pkg/composition/
  ├─ ratelimit.go       [新增] NewIPRateLimiter(opts) routing.HandlerFunc（依赖 redis.SlidingWindowAllow）
  └─ server.go          [修改] registerAuth 构造 ipLimiter 并注入 authhttp.RegisterRoutes

pkg/domains/auth/
  ├─ domain/auth.go     [修改] VerificationStore 接口新增原子校验方法签名
  ├─ infrastructure/verification_store_redis.go [修改] 实现新方法，转发到 redis.AtomicVerifyAttempt
  ├─ application/service.go  [修改] crypto/rand 生成码；VerifyCode 改为原子校验；verified TTL 收紧
  └─ interfaces/http/routes.go [修改] 为 register 子组挂 ipLimiter

pkg/shared/appctx/
  └─ context.go (+ hertzadapter/adapter.go) [修改] 新增 ClientIP() string（供框架无关中间件取 IP）

pkg/server/router/router.go  [不改]（限流按路由组挂载，不走全局）
cmd/apps/server.go            [修改] 启动顺序里加 InitRateLimitLuaScripts / InitVerifyAttemptLuaScripts
```

### 5.2 三条数据流（加固后）

**A. 发送验证码 `/send-code`**
```
请求 → L1 IP 限流(滑动窗口) → SendCode:
         邮箱格式 → 查重 → register:rate:{email} 60s 邮箱限流(保留)
         → crypto/rand 生成 6 位码 → 写 register:code:{email}(5min) → 发邮件
```

**B. 校验验证码 `/verify`**（核心修复）
```
请求 → L1 IP 限流 → VerifyCode:
         原子 verify-attempt Lua（一次 EVALSHA 完成）:
            1. 读 register:attempts:{email}；≥ MAX(5) → 返回 LOCKED（含剩余锁定秒数）
            2. 读 register:code:{email}；不存在 → 返回 EXPIRED
            3. 比对：
               ✅ 相等 → 删 code、删 attempts、置 register:verified:{email}(5min) → 返回 OK
               ❌ 不等 → INCR attempts（首次设 TTL=锁定时长）；
                        若达到 MAX → 顺带删 code（强制重发）→ 返回 WRONG + 剩余次数
```

**C. 完成注册 `/complete`**
```
请求 → L1 IP 限流 → Register:
         长度校验 → IsVerified 门控（仅 L2 通过才置位）→ 再次查重 → 建用户 → 登录 → 删 verified
```

---

## 6. 详细设计

### 6.1 通用限流原语：滑动窗口计数（IP 整形）

**算法**：加权双桶滑动窗口计数。当前桶计数 + 上一桶计数按时间衰减加权，得到估计值，与 `limit` 比较；未超则 `INCR` 当前桶。

- 边界无 2× 突刺（固定窗口的缺点被消除）。
- 每桶 key 形如 `rate:{id}:{windowStart}`，TTL = 2×window，旧桶自动过期，无需清理。

**对外 API（`pkg/server/storage/redis/ratelimit_lua.go`）**：

```go
// RateLimitResult 限流判定结果。
type RateLimitResult struct {
    Allowed   bool  // 是否放行
    Remaining int   // 本窗口剩余配额（≥0）
    RetryAfter int  // 被拒时建议的重试等待毫秒（放行时为 0）
}

// SlidingWindowAllow 滑动窗口计数限流（原子）。
//   keyPrefix: 业务前缀，如 "rl:ip:auth-register"（函数内部拼桶 key）
//   id:        主体标识，如客户端 IP
//   limit:     窗口内允许的请求数
//   window:    窗口时长（如 1 * time.Minute）
func SlidingWindowAllow(keyPrefix, id string, limit int, window time.Duration) (RateLimitResult, error)

// InitRateLimitLuaScripts 启动时预加载脚本（在 cmd/apps/server.go 调用）。
func InitRateLimitLuaScripts() error
```

> Lua 脚本草稿见 §14.1。脚本入参：当前桶 key、上一桶 key、`limit`、`window_ms`、`now_ms`、`bucket_ttl_ms`。返回 `{allowed, remaining, retryAfterMs}`。

**实现要点**：
- 复用 `like_lua.go` 的 `EvalSha` + reload-on-NOSCRIPT 模式。
- `now_ms` 由 Go 侧传入（`time.Now().UnixMilli()`），Lua 内不调用 `TIME`（避免额外往返，且与现有 `view_lua.go` 一致）。
- 桶 key 由 Go 侧按 `math.floor(now/window)*window` 计算并拼装，Lua 只负责原子计数。

### 6.2 IP 限流中间件（Hertz / 框架无关）

**为什么是 `routing.HandlerFunc` 而非 hertz 原生中间件**：现有 `RequireLogin` 即为 `routing.HandlerFunc`，由 composition 注入。限流中间件沿用同一模式，可在领域路由组内挂载，复用 `appctx.AppContext`。

**前置改动：`AppContext` 暴露 `ClientIP()`**

```go
// pkg/shared/appctx/context.go
type AppContext interface {
    context.Context
    // ... 既有方法 ...
    // ClientIP 返回客户端 IP（经可信代理解析）。
    ClientIP() string
}
// pkg/shared/appctx/hertzadapter/adapter.go
func (a *adapter) ClientIP() string { return a.c.ClientIP() }
```

> 说明：`AppContext` 已有 `Method()/Path()/Header()` 等 HTTP 概念，`ClientIP()` 与之同级，不破坏框架无关性。`Logger` 中间件早已使用 IP，此暴露只是把能力下放到领域层。

**中间件构造（`pkg/composition/ratelimit.go`）**：

```go
// IPRateLimitOpt 限流配置。
type IPRateLimitOpt struct {
    KeyPrefix string        // 如 "rl:ip:auth-register"
    Limit     int           // 窗口内上限
    Window    time.Duration // 窗口
}

// NewIPRateLimiter 返回一个 routing.HandlerFunc，按客户端 IP 做滑动窗口限流。
// 命中上限：写 429 + Retry-After + X-RateLimit-* 头并 Abort；否则放行。
func NewIPRateLimiter(opt IPRateLimitOpt) routing.HandlerFunc
```

**行为**：
- `ip := c.ClientIP()`；空 IP 兜底为 `"unknown"`（并打告警日志，可选直接 400）。
- 调 `redis.SlidingWindowAllow(opt.KeyPrefix, ip, opt.Limit, opt.Window)`。
- 被拒：`httputil.TooManyRequests(c, ...)`（复用现有 `MsgRateLimitExceeded` / `CodeTooManyRequests`），附加响应头 `Retry-After`、`X-RateLimit-Limit`、`X-RateLimit-Remaining`，`c.Abort()`。
- 放行：直接 return（hertz 链路自动继续下一个 handler，与 `RequireLogin` 一致）。

**可信代理注意**：仓库 CORS 头含 `ngrok-skip-browser-warning`，疑似经 ngrok/反代。需确保 Hertz 的 `RemoteIP`/可信代理配置正确，**不要盲目信任 `X-Forwarded-For`**（可伪造）。实施时需在 `server.Default` 选项里配置 `WithTrustedProxies`，或显式取 `c.ClientIP()`（Hertz 会按可信代理链解析）。该部署配置在 §11 风险中列出。

### 6.3 验证码尝试硬上限与锁定（修问题 1）

**Redis 键**（新增，见 §7）：

| key | 含义 | TTL |
|-----|------|-----|
| `register:code:{email}` | 验证码（既有） | 5 min |
| `register:attempts:{email}` | 该邮箱失败尝试次数 | = 锁定时长（见下） |
| `register:verified:{email}` | 已校验标记（既有） | **5 min**（原 10 min，见 §6.5） |
| `register:rate:{email}` | 发送频率（既有） | 60 s |

**参数（建议默认，可在配置覆盖）**：

| 参数 | 默认 | 说明 |
|------|------|------|
| `MaxVerifyAttempts` | 5 | 单邮箱累计失败上限 |
| `VerifyLockoutTTL` | 15 min | 达到上限后锁定时长（即 attempts key 的 TTL） |
| `VerifiedTTL` | 5 min | verified 标记有效期（由原 10 min 收紧） |

**原子 verify-attempt 脚本**（`pkg/server/storage/redis/verify_lua.go`，草稿见 §14.2）

一次 `EVALSHA` 完成「判锁 → 取码 → 比对 → 计数/置位」，杜绝并发竞态。返回码：

| 返回 | 语义 | 附带值 | handler 映射 |
|------|------|--------|-------------|
| `1` | 成功 | 0 | `httputil.Success` |
| `0` | 验证码错误（仍有剩余次数） | 剩余次数 | `BadRequest(MsgInvalidOTP)` + 可在响应里带 `remaining` |
| `-1` | 已锁定（次数耗尽） | 剩余锁定秒数 | `TooManyRequests`（语义：请稍后再试）|
| `-2` | 验证码已过期/不存在 | 0 | `BadRequest(MsgOTPExpired)` |

**锁定语义说明（UX 权衡）**：达到 5 次失败后，`attempts` key 以 15 min TTL 存续，期间即便重新 `/send-code` 拿到新码也**无法通过 `/verify`**（脚本第 1 步直接返回 LOCKED）。这是对爆破的惩罚性锁定。合法用户误触 5 次会被锁 15 min——可接受的安全姿态；若产品侧认为过严，可调低 `VerifyLockoutTTL`（如 10 min）或调高 `MaxVerifyAttempts`。

**接口扩展（`domain/auth.go`）**：

```go
type VerificationStore interface {
    // ... 既有 SetCode/GetCode/DeleteCode/MarkVerified/IsVerified/DeleteVerified
    //     SetSendRateLimit/CheckSendRateLimit ...

    // VerifyAttemptResult 原子校验结果。
    //   Status: "ok" | "wrong" | "locked" | "expired"
    //   Remaining: 剩余次数（wrong）/ 剩余锁定秒数（locked）
    // VerifyAttempt 原子地「校验验证码 + 失败计数 + 锁定 + 置 verified」。
    VerifyAttempt(email, code string) (VerifyAttemptResult, error)
}
```

`infrastructure/verification_store_redis.go` 转发到 `redis.AtomicVerifyAttempt(email, code)`。

**`application/service.go` 的 `VerifyCode` 改造**：

```go
func (s *authServiceImpl) VerifyCode(ctx context.Context, input VerifyCodeInput) error {
    r, err := s.verify.VerifyAttempt(input.Email, input.Code)
    if err != nil { return err }
    switch r.Status {
    case "ok":      return nil
    case "wrong":   return errInvalidOTP    // 仍可在响应体携带 remaining
    case "locked":  return errVerifyLocked  // 新增错误，handler 映射 429
    case "expired": return errOTPExpired
    }
    return errOTPExpired
}
```

新增错误 `errVerifyLocked` + `IsVerifyLockedErr`，并在 `handler.go` 的 `writeAuthError` 增加分支映射为 `httputil.TooManyRequests`。

### 6.4 验证码改用 crypto/rand（修问题 2）

`service.go` 的 `SendCode`：

```go
// before
// code := fmt.Sprintf("%06d", rand.Intn(1000000))   // math/rand，伪随机

// after
import "crypto/rand"
// 生成 [0, 1e6) 的密码学安全随机数
var b [8]byte
if _, err := rand.Read(b[:]); err != nil {
    return err // 或包装为内部错误
}
n := uint64(0)
for _, x := range b { n = n<<8 | uint64(x) }
code := fmt.Sprintf("%06d", n%1000000)
```

> 用 `crypto/rand.Read` 取 8 字节再模 1e6；模运算的微小偏差（< 2⁻⁵⁸ 量级）可忽略。移除 `math/rand` 导入（若仓库他处未用）。

### 6.5 收紧 verified 窗口（修问题 3）

- `registerVerifiedTTL`：`10 * time.Minute` → **`5 * time.Minute`**（与验证码 TTL 对齐，给用户填完用户名/密码仍留 5 min）。
- `Register` 逻辑不变（仍 `IsVerified` 门控 + 注册成功后 `DeleteVerified`）。
- 安全论证：问题 3 的本质是「爆破通过后 10 min 可注册」。L2 使爆破成功率 ≈ 5×10⁻⁶，verified 标记**几乎不可能**被爆破获得；即便极端获得，窗口已收紧到 5 min。残余风险可接受。
- 若需更强保证，未来上「一次性 verify token」（见 §13）。

### 6.6 限流配置（建议默认值）

| 维度 | keyPrefix | limit | window | 覆盖端点 |
|------|-----------|-------|--------|----------|
| 注册类公开端点 | `rl:ip:auth-register` | 10 | 1 min | `/send-code` `/verify` `/complete`（同前缀共享桶，防组合爆破） |
| 登录 | `rl:ip:auth-login` | 10 | 1 min | `/login`（防密码爆破） |

> 「同前缀共享桶」意味着 3 个注册端点合计 10 次/分钟/IP。如需更细，可拆为各自前缀。`limit`/`window` 走配置（见 §8），便于线上调参。

---

## 7. Redis 键设计汇总

| Key | 用途 | TTL | 写入点 | 状态 |
|-----|------|-----|--------|------|
| `register:code:{email}` | 验证码 | 5 min | SendCode | 既有 |
| `register:rate:{email}` | 发送频率限制 | 60 s | SendCode | 既有 |
| `register:verified:{email}` | 已校验标记 | **5 min**（原 10） | VerifyAttempt(ok) | TTL 改 |
| `register:attempts:{email}` | 失败尝试计数 / 锁定 | = `VerifyLockoutTTL` | VerifyAttempt(wrong) | **新增** |
| `rate:rl:ip:auth-register:{windowStart}` | IP 注册端点滑动桶 | 2 min（2×1min） | IP 中间件 | **新增** |
| `rate:rl:ip:auth-login:{windowStart}` | IP 登录滑动桶 | 2 min | IP 中间件 | **新增** |

> 桶 key 形如 `rate:{keyPrefix}:{id}:{windowStart}`，`windowStart = floor(now/window)*window`。

---

## 8. 配置项（`pkg/conf`）

新增（结构示意，字段名按现有 conf 风格）：

```yaml
auth:
  verify:
    maxAttempts: 5
    lockoutTTL: 15m
    verifiedTTL: 5m
ratelimit:
  ip:
    register: { limit: 10, window: 1m }
    login:    { limit: 10, window: 1m }
```

缺失时用上述默认值。便于线上不重启调参。

---

## 9. 受影响文件清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `pkg/server/storage/redis/ratelimit_lua.go` | 新增 | 滑动窗口脚本 + `InitRateLimitLuaScripts` + `SlidingWindowAllow` |
| `pkg/server/storage/redis/verify_lua.go` | 新增 | verify-attempt 脚本 + `Init` + `AtomicVerifyAttempt` |
| `pkg/server/storage/redis/verify.go` | 修改 | `registerVerifiedTTL` 10→5min；保留其余 |
| `pkg/server/storage/redis/constants.go` | 修改 | 新增 `RegisterAttemptsPrefix` + `GetRegisterAttemptsKey` |
| `pkg/composition/ratelimit.go` | 新增 | `NewIPRateLimiter`（`routing.HandlerFunc`） |
| `pkg/composition/server.go` | 修改 | `registerAuth` 构造并注入 `ipLimiter` |
| `pkg/domains/auth/domain/auth.go` | 修改 | `VerificationStore` 增 `VerifyAttempt` + `VerifyAttemptResult` |
| `pkg/domains/auth/infrastructure/verification_store_redis.go` | 修改 | 实现 `VerifyAttempt`，转发到 redis |
| `pkg/domains/auth/application/service.go` | 修改 | `crypto/rand`；`VerifyCode` 改原子校验 |
| `pkg/domains/auth/application/errors.go` | 修改 | 新增 `errVerifyLocked` + `IsVerifyLockedErr` |
| `pkg/domains/auth/interfaces/http/handler.go` | 修改 | `writeAuthError` 增 `locked` → 429 分支 |
| `pkg/domains/auth/interfaces/http/routes.go` | 修改 | register 子组挂 `ipLimiter` |
| `pkg/shared/appctx/context.go` | 修改 | `AppContext` 增 `ClientIP()` |
| `pkg/shared/appctx/hertzadapter/adapter.go` | 修改 | 实现 `ClientIP()` |
| `pkg/conf/*` | 修改 | 新增配置项（见 §8） |
| `cmd/apps/server.go` | 修改 | 启动链调用新 `Init*LuaScripts` |

> `routes.go` 注入 `ipLimiter` 的方式：仿照 `authCheck`，由 composition 把 `ipLimiter routing.HandlerFunc` 作为参数传入 `authhttp.RegisterRoutes`，再在路由组 `.Group("/register", ipLimiter)` 挂载。保持领域包不直接 import composition。

---

## 10. 实施步骤（建议分阶段、可分别合入）

1. **P0 — crypto/rand + verified TTL 收紧（问题 2、3 轻量部分）**
   - 改 `service.go` 生成码、改 `verify.go` TTL。低风险，独立可合。
2. **P1 — 验证码尝试硬上限（问题 1 核心）**
   - 新增 `verify_lua.go` + 常量 + 接口扩展 + service/handler 改造 + 启动注册脚本。
   - 配套单元测试（见 §11）。
3. **P2 — 通用滑动窗口原语 + IP 中间件（纵深防御）**
   - 新增 `ratelimit_lua.go` + `AppContext.ClientIP()` + `NewIPRateLimiter` + 路由挂载 + 配置。
4. **P3 — 文档与配置校准**
   - 更新 `docs/`、`configs/`、`.env.example`；线上观察 `X-RateLimit-*` 与 429 量级，调参。

每阶段互不依赖上线，可独立回滚。

---

## 11. 测试计划

**单元测试（Lua 正确性，用 miniredis 或真实 Redis）**
- 滑动窗口：边界时刻（`now` 恰为窗口起点）连续请求，验证无 2× 突刺、`remaining` 递减、被拒后 `RetryAfter > 0`。
- verify-attempt：
  - 正确码 → ok，code/attempts 被清、verified 被置（TTL 正确）。
  - 错误码 ×4 → wrong，`remaining` 递减；×5 → locked，code 被删、attempts TTL = 锁定时长。
  - locked 状态下即便新码也返回 locked。
  - 并发：N 个 goroutine 同时校验同一错误码，attempts 恰好 +N（验证原子性）。
  - 过期：code 不存在 → expired。

**接口测试（handler 层）**
- `/verify` 连续错误 → 第 5 次返 429 且带 `Retry-After`。
- `/send-code` 同 IP 高频 → 第 11 次返 429。
- `/complete` 未 verified → 仍被拒（门控不变）。

**安全验证**
- 爆破模拟：单邮箱在 5 min 内发 1000 次 `/verify`，成功次数应为 0（被锁）。
- 跨邮箱爆破：同 IP 对 100 个邮箱各试 5 次 → 应被 IP 桶拦截。

---

## 12. 风险与回滚

| 风险 | 影响 | 缓解 |
|------|------|------|
| 可信代理配置错误导致 `ClientIP` 取到网关 IP | IP 限流失效（所有请求同 IP） | 上线前确认 `WithTrustedProxies`；监控 `rl:ip:*` 桶基数；必要时退回 `RemoteIP()` |
| `EVALSHA` 命中 NOSCRIPT（Redis 重启/主从切换） | 限流/校验瞬时失败 | 复用 reload-on-NOSCRIPT 重试（既有范式）；失败兜底：限流「放行」（可用性优先），verify 返回 expired（用户重发） |
| Lua 脚本 O(N) 阻塞 Redis | 影响全局 | 本方案脚本均 O(1)；Code Review 复核 |
| 锁定策略误伤正常用户 | UX 投诉 | `MaxVerifyAttempts`/`VerifyLockoutTTL` 走配置，可线上调；响应带 `Retry-After` |
| 滑动桶 key 数量增长 | 内存 | 每桶 TTL=2×window 自动过期；按 IP 维度，规模可控 |
| 回滚 | — | P0/P1/P2 三阶段相互独立，可单独 revert；Lua 脚本未注册时 `SlidingWindowAllow`/`AtomicVerifyAttempt` 报错，需保证 `Init*LuaScripts` 与路由装配同生命周期 |

**可用性兜底原则**：限流是「可用性护栏」，Redis 故障时**限流放行、verify 降级为原逻辑（或提示重发）**，不阻断主流程。实施时在 `SlidingWindowAllow`/`NewIPRateLimiter` 的错误分支显式选择 fail-open（放行）并在日志告警。

---

## 13. 未来演进（可选，不在本期）

1. **一次性 verify token 绑定（彻底解决问题 3）**：`VerifyCode` 成功后返回不透明 token，Redis 存 `register:token:{token} → email`（TTL 5min）；`/complete` 必须携带 token，服务端 `GETDEL` 消费。使 verified 凭证不可复用、不可枚举，彻底解耦 email-key。改动含 API 契约（前端需携带 token），单列迭代。
2. **IP 限流升级为 `redis_rate`（GCRA）**：若需要更平滑的突发整形，按 §4.2 平滑替换 IP 部分。
3. **全局/账号维度的风控**：对 `/login` 失败也加账号级锁定；接入登录异常告警。
4. **回写修 `Incr` 的滑动 TTL bug**（§2.5）：评估既有调用方影响后单独修复。

---

## 14. 附录：Lua 脚本草稿（实施参考，需 review）

> 草稿用于评审与实施起步；上线前需 Code Review 并补 miniredis 用例验证。

### 14.1 滑动窗口计数（IP 整形）

```lua
-- KEYS[1] = 当前桶 key   rate:{prefix}:{id}:{curStart}
-- KEYS[2] = 上一桶 key   rate:{prefix}:{id}:{prevStart}
-- ARGV[1] = limit        (integer)
-- ARGV[2] = window_ms    (integer)
-- ARGV[3] = now_ms       (integer)
-- ARGV[4] = bucket_ttl_ms (= 2 * window_ms)
-- 返回 {allowed(1/0), remaining, retryAfterMs}
local limit    = tonumber(ARGV[1])
local window   = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])
local ttl      = tonumber(ARGV[4])

local curCount = tonumber(redis.call('GET', KEYS[1]) or '0') or 0
local prevCount= tonumber(redis.call('GET', KEYS[2]) or '0') or 0

local curStart = math.floor(now / window) * window
local elapsed  = now - curStart                 -- 进入当前窗口的毫秒数
local weight   = (window - elapsed) / window    -- 上一桶权重
local estimated= curCount + prevCount * weight

if estimated < limit then
    local n = redis.call('INCR', KEYS[1])
    redis.call('PEXPIRE', KEYS[1], ttl)
    local remaining = limit - n
    if remaining < 0 then remaining = 0 end
    return {1, remaining, 0}
else
    local retryAfter = window - elapsed         -- 当前窗口完全翻转的等待
    if retryAfter < 0 then retryAfter = 0 end
    return {0, 0, retryAfter}
end
```

### 14.2 原子 verify-attempt（验证码硬上限）

```lua
-- KEYS[1] = register:code:{email}
-- KEYS[2] = register:attempts:{email}
-- KEYS[3] = register:verified:{email}
-- ARGV[1] = inputCode       (string)
-- ARGV[2] = maxAttempts     (integer, e.g. 5)
-- ARGV[3] = lockoutTTL_sec  (integer, e.g. 900)
-- ARGV[4] = verifiedTTL_sec (integer, e.g. 300)
-- 返回 {code, value}
--   code:  1=ok, 0=wrong, -1=locked, -2=expired
--   value: ok→0; wrong→剩余次数; locked→剩余锁定秒数; expired→0
local inputCode    = ARGV[1]
local maxAttempts  = tonumber(ARGV[2])
local lockoutTTL   = tonumber(ARGV[3])
local verifiedTTL  = tonumber(ARGV[4])

local attempts = tonumber(redis.call('GET', KEYS[2]) or '0') or 0
if attempts >= maxAttempts then
    local ttl = redis.call('TTL', KEYS[2])
    if ttl < 0 then ttl = lockoutTTL end
    return {-1, ttl}
end

local storedCode = redis.call('GET', KEYS[1])
if not storedCode then
    return {-2, 0}
end

if storedCode == inputCode then
    redis.call('DEL', KEYS[1])
    redis.call('DEL', KEYS[2])
    redis.call('SET', KEYS[3], '1', 'EX', verifiedTTL)
    return {1, 0}
end

-- 错误码：计数
local n = redis.call('INCR', KEYS[2])
if n == 1 then
    redis.call('EXPIRE', KEYS[2], lockoutTTL)   -- 仅首次设 TTL（非滑动）
end
if n >= maxAttempts then
    redis.call('DEL', KEYS[1])                  -- 达上限：删码强制重发
    return {0, 0}
end
return {0, maxAttempts - n}
```

---

## 15. 调研来源

- `github.com/go-redis/redis_rate`（v10.0.1, 2023-04，GCRA，BSD-2）— https://github.com/go-redis/redis_rate
- `github.com/ulule/limiter/v3`（v3.11.2, 2023-05，固定窗口，无 Hertz 适配）— https://github.com/ulule/limiter
- `github.com/didip/tollbooth`（仅内存，令牌桶）— https://github.com/didip/tollbooth
- `github.com/throttled/throttled/v2`（GCRA）— https://github.com/throttled/throttled
- `golang.org/x/time/rate`（仅内存）— https://pkg.go.dev/golang.org/x/time/rate
- redis-cell / `CL.THROTTLE` 可移植性（AWS ElastiCache / GCP Memorystore / Azure 不支持 MODULE LOAD）
- 仓库内既有范式佐证：`pkg/server/storage/redis/like_lua.go`、`view_lua.go`、`go.mod`（Hertz v0.10.5、go-redis/v9 v9.19.0）
