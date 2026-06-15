---
name: qubar-skill
description: qubar 项目的编码结构规范（系统架构设计约束）。适用于所有对 pkg/domains、pkg/shared、pkg/composition 写入或修改的代码任务——包括新增领域、新增 API、添加 service/repository/handler、修改分层代码、跨领域调用等。凡是涉及 qubar 后端 Go 代码的编写与重构，都应先加载本技能，确保代码符合 DDD 四层架构、领域边界、依赖方向与框架无关约束。
---

# qubar 架构设计约束

本技能是 qubar 后端的**架构铁律**。项目采用 **Modular Monolith（模块化单体）+ DDD 四层架构**，目标是让每个领域包成为"准微服务"——未来拆分微服务时，把领域包整体搬出去即可，业务代码零修改。

判断任何代码改动是否符合规范，核心就看一条原则：**改完之后，这个领域包能否被整体抽出去成一个独立服务？** 如果不能，就是违反了约束。

## 1. 目录结构与领域归属

项目分三大区域，每个区域有严格的职责边界：

```
pkg/
├── shared/          共享内核：跨领域复用的基础设施（不含业务逻辑）
├── domains/         业务领域：每个子包是一个"准微服务"
└── composition/     组装层：把各领域装配起来，注册路由
```

### 1.1 `pkg/shared/` — 共享内核

只放**跨领域复用、与具体业务无关**的基础设施代码。当前包含：

| 子包 | 职责 | 能含业务逻辑吗 |
|---|---|---|
| `appctx/` | `AppContext` 接口（框架无关请求上下文） | ❌ 禁止 |
| `appctx/ginadapter/` | gin→AppContext 适配实现 | ❌ 禁止 |
| `httputil/` | 统一响应工具（`Success`/`BadRequest` 等） | ❌ 禁止 |
| `routing/` | `RouterGroup` + `HandlerFunc` 抽象 | ❌ 禁止 |

判断标准：如果一段代码只被某一个领域使用，它不属于 shared，应该放进那个领域的 `infrastructure/`。shared 是"所有领域都可能用到的公共底座"。

### 1.2 `pkg/domains/<domain>/` — 业务领域（核心）

每个领域是一个独立子包，内部固定四层结构：

```
pkg/domains/<domain>/
├── domain/              领域层：实体、值对象、常量、Repository 接口
├── application/         应用层：Service（用例编排、事务边界）
├── infrastructure/      基础设施层：Repository/Cache/ES 的具体实现
└── interfaces/
    └── http/            入站适配器：handler + 路由自注册
```

当前已建立的领域：`category`（试点完成）。待建领域见 `docs/refactor-1-migration-progress.md`。

### 1.3 `pkg/composition/` — 组装层

唯一"知道所有领域"的地方。职责：

- 构造基础设施资源（DB/Redis 连接），创建 Repository 实现并注入给 Service；
- 把各领域路由挂到 Web server 上；
- 提供框架无关的鉴权中间件（`RequireLogin`）；
- 绑定领域间的 Facade 实现（跨领域调用）。

## 2. 依赖方向铁律（最重要）

这是整个架构的核心。依赖必须**单向向下**，严禁反向：

```
interfaces/http  →  application  →  domain  ←  infrastructure
     (入站)           (编排)        (核心)       (出站实现)
```

每一层**只能 import 下列层**（已从真实代码验证）：

| 层 | 允许 import | 禁止 import |
|---|---|---|
| `domain/` | 标准库、`uuid`、`appctx`（仅类型） | gorm/redis/es、application、infrastructure、interfaces、**其他领域** |
| `application/` | `domain/`、标准库 | gorm/redis/es、infrastructure、interfaces、**其他领域** |
| `infrastructure/` | `domain/`、gorm/redis/es、标准库 | application、interfaces、**其他领域** |
| `interfaces/http/` | `application/`、`shared/appctx`、`shared/httputil`、`shared/routing`、标准库 | domain 实体的内部细节、infrastructure、**其他领域**、gin/hertz |

### 2.1 跨领域调用：只能走 Facade 接口

`domains/post` **不得** import `domains/user`。如果 post 需要查用户名，做法是：

1. 在 `domains/post/application/` 定义它需要的**最小接口**（如 `UserFacade`）；
2. 把接口作为 Service 的构造参数注入；
3. 在 `composition/` 把 `domains/user` 的实现注入给 post。

这样未来拆服务时，把 `UserFacade` 的实现从"本地调用"换成"HTTP client"即可，post 业务代码不动。

### 2.2 框架无关：领域代码不得 import gin/hertz

`pkg/domains/` 和 `pkg/shared/`（除 `appctx/ginadapter/` 外）**严禁** import `gin-gonic/gin` 或 `cloudwego/hertz`。业务代码通过 `AppContext` 接口接触 HTTP，不感知底层框架。

> 唯一例外：`pkg/shared/appctx/ginadapter/` 和 `pkg/composition/ginadapter/` 是框架适配层，允许 import gin。业务代码不会直接引用它们。

## 3. 各层的具体写法

新增领域或新增 API 时，按以下规范写每一层。**详细示例和完整代码模板见 `references/layers.md`**——写 domain/application/infrastructure/interfaces 任一层前，先读它。

### 3.1 domain 层
- 定义实体（GORM tag + TableName）和领域常量；
- 定义 `Repository`、`Cache` 等**接口**（不写实现）；
- 实体的字段必须与数据库表对齐（TableName 指向 `domains.<table>` schema）。

### 3.2 application 层
- 定义 `XxxService` **接口** + 实现；
- Service 的构造函数接收 domain 接口（`Repository`/`Cache`），不接收具体实现；
- 定义 VO/DTO（出参入参），不直接返回 domain 实体给 HTTP 层；
- 用例编排（缓存策略、调用 Repository）在这里，不在 handler 里。

### 3.3 infrastructure 层
- 实现 domain 定义的 `Repository`/`Cache` 接口；
- 构造函数接收 `*gorm.DB` / redis client 等基础设施资源；
- 返回 domain 层的类型（`[]domain.Category`），不返回 GORM 内部类型。

### 3.4 interfaces/http 层
- `handler.go`：handler 方法接收 `appctx.AppContext`（**不是** `*gin.Context`）；
- 用 `c.BindJSON/BindQuery` 绑定参数，用 `httputil.Success/InternalError` 写响应；
- `routes.go`：提供 `RegisterRoutes(rg routing.RouterGroup, svc application.XxxService, authCheck routing.HandlerFunc)`，领域自己挂路由。

## 4. 装配与注册

新增领域后，在 `pkg/composition/server.go` 的 `RegisterDomainRoutes` 里追加一个 `registerXxx(root, deps, authCheck)` 调用，按 `registerCategory` 的模式写装配链：

```go
func registerXxx(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
    repo := xxxinfra.NewXxxRepository(deps.DB.Get())
    cache := xxxinfra.NewXxxCache()
    svc := xxxapp.NewXxxService(repo, cache)
    xxxhttp.RegisterRoutes(root, svc, authCheck)
}
```

## 5. 鉴权

用 `composition.RequireLogin`（框架无关，基于 `stputil` 直校验），**不要**用 `sagin.CheckLogin()`（绑定 gin 中间件链，无法跨框架复用）。需要鉴权的路由组在 `RegisterRoutes` 里把 `RequireLogin` 作为中间件挂上。

## 6. 过渡期说明（重要）

项目正在从旧 MVC 结构（`pkg/server/{controller,model,storage,router}`）迁移到新 DDD 结构（`pkg/domains/`）。迁移期间：

- **已搬迁的领域**：走 `pkg/domains/` + `composition/`。如 `category`。
- **未搬迁的领域**：仍走 `pkg/server/controller` + `model` + `router/routers.go`。如 `user/circle/post/comment/like/auth`。

新增功能时：如果属于已搬迁领域，严格按本技能约束写；如果属于未搬迁领域，可暂时沿用旧结构，但**新增领域应直接按 DDD 结构建立**。迁移进度见 `docs/refactor-1-migration-progress.md`。

## 何时读 references/

- 写 domain/application/infrastructure/interfaces 任一层、需要看完整代码模板 → 读 `references/layers.md`
- 需要理解 AppContext 的全部方法、写自定义中间件 → 读 `references/appctx.md`（如存在）
- 需要判断"某段代码该放哪个领域/哪一层" → 先读本技能 §1 §2，仍不确定再读 `references/`
