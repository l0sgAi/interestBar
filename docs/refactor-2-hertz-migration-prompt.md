# Gin → Hertz 迁移提示词（用于新会话执行）

> 本文件是给「新会话」的启动提示词。可直接复制下方「---BEGIN PROMPT---」到「---END PROMPT---」之间的内容粘贴给新会话。

---

## 背景说明（给人类阅读，不要粘贴给新会话）

经过重构一（批次 A/B/C），项目已完成 DDD 模块化单体改造，**业务层已完全框架无关**：
- `pkg/shared/appctx/`：框架无关的请求上下文接口（gin 实现在 `ginadapter/`）
- `pkg/shared/routing/`：框架无关的路由抽象
- `pkg/shared/httputil/`：框架无关的响应工具
- `pkg/domains/*/`：8 个业务领域**零 gin import**，全部通过 `appctx.AppContext` 工作
- `pkg/composition/auth.go` 的 `RequireLogin` **已改用 `stputil`（框架无关）**，不再依赖 `sagin.CheckLogin()`

因此 Hertz 迁移的范围**远小于**原始 `refactor-2-gin-to-hertz-migration.md` 文档预估（原文档假设 M1=AppContext 抽象尚未做）。现在 M1 已完成，剩余工作集中在「胶水层」：
- sa-token 初始化（`sa_token_init.go` 当前用 `sagin.DefaultConfig/NewManager/SetManager`）
- composition 层路由适配（`composition/ginadapter/` + `composition/server.go`）
- AppContext 的 gin 实现（`shared/appctx/ginadapter/`）
- 4 个中间件（`pkg/server/router/middleware/`）
- 路由入口（`pkg/server/router/router.go`）
- 服务启动（`cmd/apps/server.go`）

**核心难点收敛为一点**：sa-token 的 `sagin.DefaultConfig/NewManager/SetManager` 用的是 `integrations/gin` 包。迁移到 hertz 需要自写一个 hertz 版 sa-token adapter（实现 `core/adapter.RequestContext` 接口），或确认 sa-token-go 是否提供了框架无关的初始化路径。

---

---BEGIN PROMPT---

# 任务：执行 Gin → Hertz 框架迁移（重构二）

## 项目背景

这是一个 Go 项目（模块名 `interestBar`，工作目录 `/Users/losgai/codes/golang/qubar`），当前 Web 框架是 **Gin v1.11.0**，需要迁移到 **CloudWeGo Hertz**。

**重要前提**：项目刚完成 DDD 模块化单体重构（批次 A/B/C），业务层**已经完全框架无关**。请先阅读以下文件理解现状，**不要假设业务代码还耦合 gin**：

### 必读文件（理解现有架构，动手前完整阅读）
1. `docs/refactor-1-migration-progress.md` —— 重构一的完成状态、架构守护规则、已建立的抽象
2. `docs/refactor-2-gin-to-hertz-migration.md` —— Hertz 迁移方案（**注意：此文档写于重构一之前，其中"M1: AppContext 抽象"已完成，请以代码现状为准**）
3. `pkg/shared/appctx/context.go` —— 框架无关请求上下文接口（**已存在，无需新建**）
4. `pkg/shared/appctx/ginadapter/adapter.go` —— gin 版 AppContext 实现（**这是要替换的**）
5. `pkg/shared/routing/group.go` —— 框架无关路由抽象（**已存在**）
6. `pkg/shared/httputil/response.go` —— 框架无关响应工具（**已存在，依赖 AppContext**）
7. `pkg/composition/server.go` —— 装配层，`RegisterDomainRoutes(e *gin.Engine)` 是迁移核心改动点
8. `pkg/composition/ginadapter/group.go` —— gin 版 RouterGroup 实现（**这是要替换的**）
9. `pkg/composition/auth.go` —— `RequireLogin` 中间件（**已用 stputil，框架无关，无需改逻辑**，只需适配签名）
10. `pkg/server/auth/sa_token_init.go` —— sa-token 初始化（**当前依赖 `integrations/gin`，这是核心难点**）
11. `pkg/server/router/router.go` —— 路由入口（`gin.New()` + 中间件 + `composition.RegisterDomainRoutes`）
12. `pkg/server/router/middleware/{log,cors,csrf,cache}.go` —— 4 个 gin 中间件
13. `cmd/apps/server.go` —— 服务启动（`r.Run(addr)` + 优雅退出）
14. `go.mod` —— 当前依赖

### 架构守护规则（迁移后必须仍然成立，每步都要验证）
```
✅ pkg/domains/ 下零 gin import（已是现状，迁移不能破坏）
✅ pkg/domains/ 下零 sa-token-go/integrations import（已是现状）
✅ 领域间零跨域 domain/infrastructure import
✅ domain 层只依赖标准库 + uuid + sql/driver + encoding/json
✅ go build ./... / go vet ./... / go test ./... 全部通过
```

迁移的最终验证标准：`grep -r "gin-gonic/gin" pkg/` 为空，且上述守护全部通过。

## 现状关键事实（避免重复劳动）

1. **AppContext 抽象已完成**（`pkg/shared/appctx/context.go`）。所有领域 handler 只依赖 `appctx.AppContext` 接口，**业务代码迁移时一行都不用改**。
2. **RequireLogin 已用 stputil**（`pkg/composition/auth.go`），不依赖 `sagin.CheckLogin()`。鉴权中间件的业务逻辑无需改，只需适配 hertz 的 HandlerFunc 签名。
3. **gin 依赖只存在于「胶水层」**（约 8 个文件）：
   - `pkg/shared/appctx/ginadapter/adapter.go`
   - `pkg/composition/ginadapter/group.go`
   - `pkg/composition/server.go`（`RegisterDomainRoutes(e *gin.Engine)`）
   - `pkg/server/router/router.go`、`routers.go`（注：routers.go 已在批次C删除）
   - `pkg/server/router/middleware/{log,cors,csrf,cache}.go`
   - `pkg/server/auth/sa_token_init.go`（`sagin.DefaultConfig/NewManager/SetManager`）
   - `cmd/apps/server.go`（`r.Run(addr)`）

## 执行策略（分批进行，每批后 build + vet + test）

### 前置：先用 EnterPlanMode 探索 sa-token-go 源码确认可行性
sa-token-go 的 `integrations/gin` 包提供了 `DefaultConfig/NewManager/SetManager/GetManager`。迁移到 hertz 前，**必须先确认**：
- sa-token-go 是否有框架无关的初始化路径（直接用 `core` 包构造 manager，不经过 `integrations/gin`）？
- 还是需要自写 hertz adapter（实现 `core/adapter.RequestContext` 接口）？

阅读 `github.com/click33/sa-token-go` 的源码（在 `$GOPATH/pkg/mod/` 下），重点看：
- `core/` 包：`SaTokenManager`、`SaTokenContext`、`adapter.RequestContext` 接口
- `integrations/gin/` 包：`NewManager`/`DefaultConfig`/`SetManager` 如何用 gin 构造 manager
- `stplogic` / `stputil`：它们是否真的只依赖 `adapter.RequestContext` 而不依赖具体框架

把结论写进 plan，再决定是「自写 hertz adapter」还是「直接用 core 包构造 + 注入 hertz context adapter」。

### 推荐批次划分（每批独立可验证）

**批次 0：探索 + 设计（用 EnterPlanMode）**
- 阅读 sa-token-go 源码，确认初始化路径
- 设计 hertz 版 AppContext 适配器（`pkg/shared/appctx/hertzadapter/`）
- 设计 hertz 版 RouterGroup 适配器（`pkg/composition/hertzadapter/`）
- 设计 sa-token hertz 集成方案
- 用 ExitPlanMode 提交计划给我评审

**批次 1：新增 hertz 适配层（不删 gin，双轨并行）**
- `go.mod` 加入 `cloudwego/hertz`
- 新增 `pkg/shared/appctx/hertzadapter/adapter.go`：把 `*app.RequestContext` 适配成 `appctx.AppContext`
- 新增 `pkg/composition/hertzadapter/group.go`：把 hertz engine/group 适配成 `routing.RouterGroup`
- 新增 sa-token hertz 集成（`pkg/server/auth/sa_token_hertz.go` 或内联到 init）
- 验证：`go build ./...` 通过（新代码不影响 gin 现有路径）

**批次 2：切换入口到 hertz**
- 改 `pkg/server/router/router.go`：用 `server.Default()` 替代 `gin.New()`；中间件改用 hertz 签名
- 改 4 个中间件（log/cors/csrf/cache）为 hertz 版本（**注意 hertz HandlerFunc 签名是 `func(ctx context.Context, c *app.RequestContext)`**）
- 改 `composition/server.go`：`RegisterDomainRoutes` 改为接收 hertz engine，内部用 `hertzadapter` 适配
- 改 `cmd/apps/server.go`：`r.Run(addr)` → hertz 的 `h.Spin()` 或自管信号 + `Serve()`
- 验证：server 能起来，至少 health/一个领域接口通

**批次 3：联调 + 删除 gin**
- 全量接口回归（重点：OAuth 回调、文件上传 multipart、点赞 Lua、CSRF）
- 删除 `pkg/shared/appctx/ginadapter/`、`pkg/composition/ginadapter/`
- 删除 `pkg/server/response/`（已被 `httputil` 取代，仅 csrf 中间件还引用——确认 csrf 是否仍需要）
- `go.mod` 移除 `gin-gonic/gin`、`gin-contrib/sse`、`click33/sa-token-go/integrations/gin`
- 运行架构守护 + `grep -r "gin" pkg/` 为空

## Hertz 迁移的技术坑点（务必注意）

1. **HandlerFunc 签名不同**：gin 是 `func(c *gin.Context)`，hertz 是 `func(ctx context.Context, c *app.RequestContext)`（两个参数）。
2. **GetHeader 返回 `[]byte`**：hertz 的 `c.GetHeader(k)` 返回 `[]byte`，需 `string(...)` 转换。AppContext 适配器里统一处理。
3. **context 语义**：hertz 的 `app.RequestContext` 是值类型，取标准 ctx 要 `c.Request.Context()`。AppContext 实现里要注意。
4. **状态码常量**：用 `consts.StatusXxx` 而非裸数字。
5. **Bind**：`c.ShouldBindJSON(&x)` → `c.Bind(&x)`（hertz 默认按 Content-Type 自动选择 binder）。
6. **ClientIP**：`c.ClientIP()` → `c.RemoteIP()`（可能需配合 `clientip` 中间件处理 X-Forwarded-For）。
7. **JSON 字面量**：`gin.H` → `map[string]any` 或 hertz 的 `app.H`。建议统一用 `map[string]any`。
8. **优雅退出**：gin 用 signal 自管；hertz 的 `h.Spin()` 自带信号处理，或用 `server.Spin()`。当前 `cmd/apps/server.go` 的 signal 逻辑要调整。
9. **Multipart 上传**：重点测试 storage 领域的文件上传（`FormFile`/`MultipartForm`），hertz 的 multipart 实现与 gin 不同。
10. **netpoll vs go net**：macOS dev 环境建议用 go net（`server.WithTransport(transport.New(nil))` 或默认），生产 Linux 用 netpoll。

## 产出要求

1. 每个批次结束后：`go build ./... && go vet ./... && go test ./...` 全过，并运行架构守护脚本。
2. 每个批次给我一份变更总结（改了哪些文件、为什么、行为是否变化）。
3. 遇到 sa-token 集成的关键设计决策（如「自写 adapter vs 用 core 包」）时，先用 AskUserQuestion 跟我确认再动手。
4. 迁移完成后更新 `docs/refactor-2-gin-to-hertz-migration.md` 的状态为「已完成」，记录实际改动与原方案的差异。

## 现在开始

请先用 EnterPlanMode 进入计划模式，阅读上述必读文件 + sa-token-go 源码，产出一份详细的迁移计划给我评审。**不要直接开始改代码。**

---END PROMPT---
