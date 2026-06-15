# 重构二：Gin → Hertz 框架迁移方案

> 状态：**计划文档（待评审）** · 日期：2026-06-15
> 适用代码基线：`feature/hertz-ddd-port-20260615`（Gin v1.11.0）

---

## 一、结论先行（TL;DR）

| 评估项 | 结论 |
|---|---|
| **可行性** | ✅ **可行**。无技术硬阻塞。 |
| **核心难点** | `sa-token-go` 目前**只提供 gin 集成**（`integrations/gin`），没有官方 hertz 适配。但 sa-token-go 内部已有 `core/adapter.RequestContext` 抽象，**自写一个 hertz adapter ≈ 100 行**即可复用全部 sa-token 能力。 |
| **工作量** | 中等。约 **3–5 人天**（含自写 sa-token hertz adapter + 20 个文件改写 + 联调）。 |
| **风险** | 中低。最大风险点是 sa-token hertz adapter 的正确性（需对照 gin adapter 写测试）。 |
| **推荐策略** | **先抽 Web 框架无关层（AppContext/Response/Router 接口），再换底层实现**。这样业务代码只改一次，且能和"重构一（DDD 改良）"合并推进。 |

---

## 二、迁移动机与可行性分析

### 2.1 为什么要换 Hertz？（动机核对）

迁移框架是有成本的，先确认动机是否成立：

| 动机 | 是否成立 | 说明 |
|---|---|---|
| **性能** | ✅ 部分成立 | Hertz 默认基于 netpoll（非 go net），在长连接/高 QPS 场景下吞吐显著优于 gin。字节内部生产验证。 |
| **生态对齐 CloudWeGo** | ✅ 成立 | 若后续引入 Kitex（RPC）、Volo、Eino 等 CloudWeGo 组件，技术栈统一。 |
| **中间件/扩展性** | ✅ 成立 | Hertz 的中间件、流式、泛化调用、代码生成（hz 工具）更现代。 |
| **项目已埋伏笔** | ✅ 成立 | 当前分支名 `feature/hertz-ddd-port-20260615` 表明团队已有 hertz 意图。 |

> ⚠️ **反向提醒**：如果只是想"换个快一点的框架"而没有 CloudWeGo 生态诉求，性价比不一定高。建议确认动机是 **生态对齐** 或 **实测有性能瓶颈**，否则迁移的 ROI 偏低。本方案假定动机成立，给出"如何做"。

### 2.2 技术可行性逐项核对

| 技术点 | 当前依赖 | Hertz 对应 | 可行性 |
|---|---|---|---|
| 路由 | `gin.Engine` / `gin.RouterGroup` | `server.Hertz` / `*route.Group` | ✅ API 几乎对称 |
| Context | `*gin.Context` | `app.RequestContext` | ✅ 注意：hertz 的 context 是值类型语义，需 `ctx := c.Request.Context()` |
| 参数绑定 | `c.ShouldBindJSON/Query` | `c.Bind` / `bind.Query`（hertz 提供 `binding` 子包） | ✅ tag 兼容（`json`/`form`/`binding`） |
| JSON 响应 | `c.JSON(code, gin.H{...})` | `c.JSON(code, h.H{...})` / `resp.Json` | ✅ |
| Path 参数 | `c.Param("id")` | `c.Param("id")` | ✅ 完全一致 |
| Header/Cookie | `c.GetHeader` / `c.Cookie` | `c.GetHeader` / `c.Cookie` | ✅ |
| 中间件签名 | `gin.HandlerFunc` (`func(*gin.Context)`) | `app.Handler` (`func(context.Context, *app.RequestContext)`) | ⚠️ 签名不同，需统一改写 |
| Recovery | `gin.Recovery()` | `server.Recovery(mw...)`（hertz 内置） | ✅ |
| CORS | 自写 `middleware.CORS()` | 可继续自写（参数从 `c.Writer.Header()` 改为 `c.Header`） | ✅ |
| **sa-token-go** | `integrations/gin` 的 `CheckLogin()` 等 | **无官方 hertz 适配** | ⚠️ **需自写 adapter（核心工作）** |
| OAuth (golang.org/x/oauth2) | 与框架无关 | 不受影响 | ✅ |
| GORM / Redis / ES / S3 / Redpanda | 与框架无关 | 不受影响 | ✅ |

### 2.3 sa-token-go 的关键发现（决定可行性）

阅读源码确认：
```
github.com/click33/sa-token-go/integrations/gin@v0.1.7/
├── context.go     ← GinContext 实现 core/adapter.RequestContext 接口
├── plugin.go      ← 把 gin.Context 包成 GinContext 后调 stplogic
├── annotation.go  ← CheckLogin/CheckRole 等 gin.HandlerFunc 装饰器
└── export.go      ← NewManager / SetManager / DefaultConfig
```

**核心结论**：sa-token-go 的业务逻辑全在 `stplogic.go` / `stputil.go` 里，通过 `adapter.RequestContext` 抽象与具体 Web 框架解耦。`integrations/gin` 只是把 `*gin.Context` 适配成 `adapter.RequestContext`。**只要为 hertz 写一个等价的 adapter，就能 100% 复用 stputil 的所有能力**（Login/Logout/GetSession/CheckLogin 等）。

这也意味着：`stputil.Login()` / `stputil.GetSession()` / `stputil.GetLoginID(token)` 这些**不依赖 context 的工具函数完全不用改**。只有 `CheckLogin()` 等**中间件装饰器**需要重写为 hertz 版本。

---

## 三、改动清单（逐文件）

### 3.1 新增文件

| 文件 | 作用 | 工作量 |
|---|---|---|
| `pkg/server/auth/sa_token_hertz.go`（或 `pkg/shared/platform/satoken/hertz/`） | hertz 版 sa-token adapter：实现 `adapter.RequestContext` + 提供 `CheckLogin()/CheckRole()` 等 `app.HandlerFunc` 装饰器 | ~120 行（对照 gin adapter 抄） |
| `pkg/server/auth/sa_token_hertz_test.go` | 对照 gin adapter 的测试，验证 Login→CheckLogin 闭环 | ~80 行 |
| `pkg/server/server.go`（或 `composition/server.go`） | hertz server 初始化（替代 `router.InitRouter` 返回 `*gin.Engine`） | ~40 行 |

### 3.2 改写文件（20 个）

按改动模式分组：

#### A. 入口与路由（3 个）

| 文件 | 改动 |
|---|---|
| `cmd/apps/server.go` | `r.Run(addr)` → hertz 的 `server.WithHostPorts(addr).Serve()`；注意 hertz 需优雅退出用 `server.Spin()` 或自己包 signal |
| `pkg/server/router/router.go` | `gin.New()` → `server.Default(...)`；中间件 `r.Use(...)` 签名兼容 |
| `pkg/server/router/routers.go` | `func RegisterRoutes(r *gin.RouterGroup)` → `func RegisterRoutes(r *route.Group)`；`sagin.CheckLogin()` → `hertzSaToken.CheckLogin()` |

#### B. 中间件（4 个）

| 文件 | 改动 |
|---|---|
| `middleware/log.go` | `func(c *gin.Context)` → `func(ctx context.Context, c *app.RequestContext)`；`c.Writer.Status()` → `c.Response.StatusCode()`；`c.ClientIP()` → `clientip.Calibrate(c.RemoteIP())` |
| `middleware/cors.go` | `c.Writer.Header().Set(...)` → `c.Header.Set(...)`；`c.AbortWithStatus(204)` → `c.AbortWithStatus(consts.StatusOK)` 注意 hertz 常量；`c.Request.Method` → `string(c.Method())` |
| `middleware/csrf.go` | 全部 `*gin.Context` → hertz；`c.GetHeader` → `c.GetHeader`；`c.Set/Get` → `c.Set/Get`（注意 hertz 的 Set/Get 用泛型，类型安全） |
| `middleware/cache.go` | 空实现，直接换签名 |

#### C. 响应工具（1 个）

| 文件 | 改动 |
|---|---|
| `pkg/server/response/response.go` | 所有 `c *gin.Context` → hertz context；`c.JSON(code, ...)` → `c.JSON(code, ...)`（hertz 用 `resp.Json`，但 `app.RequestContext` 自带 `JSON` 方法） |

**建议**：这一层最好顺手抽成接口（见 §4），让 `response` 不直接依赖任何框架。

#### D. 工具层（1 个）

| 文件 | 改动 |
|---|---|
| `pkg/server/utils/sa_token_util.go` | `GetLoginIDFromRequest(c *gin.Context)` → 抽象成接收"能取 header 的接口"，或直接换成 hertz context |

#### E. Controller 层（12 个）

所有 controller 文件的改动模式**高度机械**：

```go
// 旧
func (ctrl *XController) Foo(c *gin.Context) {
    var req FooRequest
    if err := c.ShouldBindJSON(&req); err != nil { ... }
    response.Success(c, data)
}

// 新
func (ctrl *XController) Foo(ctx context.Context, c *app.RequestContext) {
    var req FooRequest
    if err := c.Bind(&req, binding.JSON); err != nil { ... }   // 或 c.JSON 序列化
    response.Success(c, data)
}
```

涉及文件：`controller/{user,circle,post,comment,like,category,login,register,oauth,upload,hello,controller}.go`

**机械化替换点**（全项目约 89 处）：
- `c.ShouldBindJSON(&x)` → `c.Bind(&x, gojson.Name("json"))` 或 hertz 默认
- `c.ShouldBindQuery(&x)` → `c.Bind(&x, query.Tag("form"))`
- `c.Param("id")` → `c.Param("id")`（不变）
- `c.GetHeader(k)` → `c.GetHeader(k)`（注意返回类型，hertz 是 `[]byte`，要 `string()`）
- `c.Query(k)` → `c.Query(k)`
- `c.PostForm(k)` → `c.PostForm(k)`
- `c.JSON(code, gin.H{...})` → `c.JSON(code, h.H{...})`（hertz 提供 `pkg/app.H`）
- `gin.H` → `h.H`（或用 `map[string]any`）

> ⚠️ **最大坑点**：hertz 的 `GetHeader` 返回 `[]byte` 而非 `string`。涉及 `utils.GetLoginIDFromRequest` 里 `c.GetHeader(tokenName)` 等处需 `string(...)` 转换。建议在 adapter 里统一处理。

### 3.3 go.mod 改动

```
新增：
  github.com/cloudwego/hertz v0.9.x
  （hertz 自带 bytedance/sonic，已有的 sonic 依赖可复用）

移除：
  github.com/gin-gonic/gin
  github.com/gin-contrib/sse
  github.com/click33/sa-token-go/integrations/gin   ← 改用自写 hertz adapter
```

sa-token-go 的 `stputil` / `storage/redis` / `core` **保留不动**。

---

## 四、推荐做法：先抽 Web 无关层，再换引擎

直接把 20 个文件的 `gin.Context` 全改成 `app.RequestContext`，虽然能跑，但有两个问题：
1. **耦合反向**：业务层直接依赖 hertz 类型，未来再换框架（或同重构一合并）又要大改。
2. **和重构一（DDD）冲突**：领域代码不该知道 Web 框架是谁。

**推荐分两步**：

### 步骤 1：引入 Web 无关抽象（0.5 天）

新增 `pkg/shared/appctx/`：

```go
// pkg/shared/appctx/context.go
type AppContext interface {
    context.Context

    // 请求信息
    Method() string
    Path() string
    Param(name string) string
    Query(name string) string
    Header(name string) string
    PostForm(name string) string

    // 绑定
    BindJSON(v any) error
    BindQuery(v any) error

    // 响应
    JSON(code int, v any)
    SetHeader(k, v string)

    // 业务上下文（由中间件填充）
    UserID() (uuid.UUID, bool)
    LoginID() (string, bool)
    SetUserID(id uuid.UUID)
}
```

并提供一个 gin 实现作为过渡：
```go
// pkg/shared/appctx/gin_adapter.go
type ginAppContext struct{ *gin.Context }
func (g *ginAppContext) BindJSON(v any) error { return g.Context.ShouldBindJSON(v) }
func (g *ginAppContext) UserID() (uuid.UUID, bool) { ... }
// ...
```

同时把 `response` 包改为接收 `AppContext`：
```go
func Success(c AppContext, data any) { c.JSON(200, Response{...}) }
```

**此步完成后**：所有 controller 只依赖 `AppContext`，不依赖 gin。先合并、先跑通，行为零变化。

### 步骤 2：替换底层引擎为 Hertz（2–3 天）

1. 新增 `pkg/shared/appctx/hertz_adapter.go`：把 `*app.RequestContext` 适配成 `AppContext`。
2. 新增 sa-token hertz adapter（§3.1）。
3. 改 `router/router.go`、`router/routers.go`、`server.go` 用 hertz。
4. 改 4 个中间件的底层类型（`AppContext` 层不动）。
5. controller 代码**几乎不动**（因为已经只依赖 `AppContext`）。
6. 删除 `gin_adapter.go`、`integrations/gin` 依赖。

**收益**：业务代码在步骤 1 就已经"框架无关"，步骤 2 只动"胶水层"，回归测试面极小。

---

## 五、sa-token Hertz Adapter 设计（核心代码骨架）

这是本次迁移**唯一有创造性**的工作。对照 `integrations/gin/context.go` 抄写：

```go
// pkg/shared/platform/satoken/hertz/context.go
package hertz

import (
    "context"

    "github.com/click33/sa-token-go/core/adapter"
    "github.com/cloudwego/hertz/pkg/app"
)

// hertzContext 把 hertz 的 *app.RequestContext 适配为 sa-token 的 adapter.RequestContext
type hertzContext struct {
    c *app.RequestContext
}

func New(c *app.RequestContext) adapter.RequestContext {
    return &hertzContext{c: c}
}

func (h *hertzContext) GetHeader(key string) string  { return string(h.c.GetHeader(key)) }
func (h *hertzContext) GetQuery(key string) string   { return string(h.c.Query(key)) }
func (h *hertzContext) GetCookie(key string) string  { return string(h.c.Cookie(key)) }
func (h *hertzContext) SetHeader(k, v string)        { h.c.Header.Set(k, v) }
func (h *hertzContext) SetCookie(name, val string, maxAge int, path, domain string, secure, httpOnly bool) {
    h.c.SetCookie(name, val, maxAge, path, domain, secure, httpOnly)
}
// ... 其余方法照抄 gin adapter 的实现，把 g.c.GetHeader 换成 string(h.c.GetHeader)
```

```go
// pkg/shared/platform/satoken/hertz/middleware.go
package hertz

import (
    "context"

    "github.com/click33/sa-token-go/core/stp"
    "github.com/cloudwego/hertz/pkg/app"
)

// CheckLogin 对照 integrations/gin/annotation.go 的 CheckLogin 实现
func CheckLogin() app.HandlerFunc {
    return func(ctx context.Context, c *app.RequestContext) {
        rc := New(c)
        // 调用 sa-token 的 stplogic 做校验（与 gin 版逻辑完全一致）
        if err := stp.CheckLogin(rc); err != nil {
            c.AbortWithStatus(401)  // 或用项目自定义的 response.Unauthorized
            return
        }
        c.Next(ctx)
    }
}

// CheckRole / CheckPermission 同理
```

> **注意**：上面是骨架。真正落地时要打开 `integrations/gin/annotation.go` 和 `plugin.go` 逐行对照，因为 sa-token 的 CheckLogin 实际调用的是 manager 上的某个方法，gin adapter 里做了 context 包装与 abort 处理。**建议先跑通"Login → CheckLogin"的最小闭环测试再全面替换。**

---

## 六、执行计划与里程碑

| 阶段 | 内容 | 工时 | 验收标准 |
|---|---|---|---|
| **M1** | 抽 `AppContext` 接口 + gin 实现；`response` 改用 `AppContext`；controller 全量切换为 `AppContext` | 1 人天 | 编译通过，全接口回归测试通过，行为零变化（仍是 gin） |
| **M2** | 写 sa-token hertz adapter + 单测（对照 gin adapter） | 1 人天 | 单测覆盖 Login/CheckLogin/CheckRole/Session 读写 |
| **M3** | 引入 hertz，写 hertz 版 `AppContext` 适配器、路由初始化、4 个中间件 | 1 人天 | hertz server 能起来，health 接口通 |
| **M4** | sa-token 中间件接入 hertz；路由全量切到 hertz | 0.5 人天 | 登录/登出/需鉴权接口全部通过 |
| **M5** | 联调 + 性能对比 + 删除 gin 依赖 | 1 人天 | `go.mod` 不再含 `gin-gonic/gin`；接口压测无回退 |
| **合计** | | **4.5 人天** | |

> 若与重构一（DDD）合并推进，M1 的 `AppContext` 可直接复用为 DDD 改良的"interfaces 层契约"，两者协同可省 1–2 人天。

---

## 七、风险与应对

| 风险 | 等级 | 应对 |
|---|---|---|
| sa-token hertz adapter 写错（如 header 返回 `[]byte` 未转 string） | 中 | M2 强制单测，对照 gin adapter 逐方法验证；先在测试环境灰度 |
| hertz 的 `GetRawData` / Body 读取语义与 gin 不同（如 Bind 后 Body 被消费） | 中 | 重点测试 upload（文件上传）和 register（JSON body）接口 |
| CSRF 中间件依赖 `c.Set/Get`，hertz 的 Set/Get 是泛型 API，类型语义不同 | 中 | 仔细对照改写，加集成测试覆盖 POST 流程 |
| hertz 的 netpoll 在某些环境（如 macOS dev）行为与 Linux 不同 | 低 | 开发机用 go net（hertz 默认回退），生产用 netpoll；CI 在 Linux 跑 |
| `c.ClientIP()` 在 hertz 是 `c.RemoteIP()`，且需 `clientip` 中间件处理 X-Forwarded-For | 低 | log 中间件改写时一并处理 |
| gin.H / h.H 字面量散落各处 | 低 | 用 `map[string]any` 统一，或保留 h.H |

---

## 八、验证清单（迁移完成判定）

- [ ] `go.mod` 不再包含 `github.com/gin-gonic/gin`
- [ ] 全项目 `grep -r "gin" pkg/` 为空（除注释）
- [ ] 所有现有 HTTP 接口回归测试通过（建议用 curl/httpie 跑一遍 e2e，重点：OAuth 登录回调、文件上传、点赞、CSRF 流程）
- [ ] sa-token 的 CheckLogin 在无 token / 错 token / 正确 token 三种情况下行为与迁移前一致
- [ ] 压测：同等条件下 hertz 的 QPS ≥ gin（若反而下降，排查是否误用了 go net）
- [ ] 日志中间件输出格式不变（status/path/cost 字段齐全）
- [ ] 优雅退出（SIGTERM 后连接 drain）正常

---

## 九、与重构一（DDD 改良）的协同

强烈建议两个重构**串行合并推进**，执行顺序：

```
[重构二 M1: AppContext 抽象]
        ↓ （产出 shared/appctx，业务层框架无关）
[重构一 阶段0-1: shared 内核 + Repository 接口]  ← 复用 AppContext
        ↓
[重构二 M2-M5: 换 hertz 引擎]                    ← 只动 interfaces/http 层
        ↓
[重构一 阶段2-4: 逐领域搬迁 + 架构守护]
```

**协同收益**：
- AppContext 抽象是两者的共同前置，做一次即可。
- hertz 迁移完成后，DDD 的 `interfaces/http/handler.go` 直接用 `AppContext`，业务层与框架完全解耦。
- 两个重构合并总工时约 **2.5 周**，比分两次做（~3.5 周）省一周。

---

## 附录：关键 API 对照速查表

| 能力 | Gin | Hertz |
|---|---|---|
| 引擎 | `gin.New()` / `gin.Default()` | `server.Default()` / `server.New()` |
| 启动 | `r.Run(":8080")` | `h.Spin()`（自带信号处理） |
| 路由组 | `r.Group("/x")` | `h.Group("/x")`（返回 `*route.Group`） |
| Handler 签名 | `func(c *gin.Context)` | `func(ctx context.Context, c *app.RequestContext)` |
| 中间件 | `gin.HandlerFunc` | `app.HandlerFunc` |
| Path 参数 | `c.Param("id")` | `c.Param("id")` |
| Query | `c.Query("k")` | `string(c.Query("k"))` |
| Header | `c.GetHeader("k")` (string) | `string(c.GetHeader("k"))` (**[]byte**) |
| JSON | `c.JSON(200, gin.H{})` | `c.JSON(200, h.H{})` / `resp.Json` |
| 绑定 JSON | `c.ShouldBindJSON(&x)` | `c.Bind(&x, binding.JSON)` |
| 绑定 Query | `c.ShouldBindQuery(&x)` | `c.Bind(&x, goquery.Tag("form"))` |
| 状态码 | `c.Status(204)` | `c.SetStatusCode(204)` |
| 中止 | `c.AbortWithStatus(401)` | `c.AbortWithStatus(consts.StatusUnauthorized)` |
| Client IP | `c.ClientIP()` | `c.RemoteIP()`（配合 `clientip` 中间件） |
| 请求体 | `c.GetRawData()` | `c.Body()` |
| Cookie 设置 | `c.SetCookie(...)` | `c.SetCookie(...)` |
| 上下文存取 | `c.Set/Get` | `c.Set/Get`（泛型） |
| Recovery | `gin.Recovery()` | `server.Recovery(mw...)` |
