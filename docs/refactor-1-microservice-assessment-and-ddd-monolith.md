# 重构一：微服务拆分评估 + DDD 单体架构改良方案

> 状态：**计划文档（待评审）** · 作者：架构评估 · 日期：2026-06-15
> 适用代码基线：`feature/hertz-ddd-port-20260615`（71 个 Go 文件，约 1.3 万行业务代码）

---

## 一、结论先行（TL;DR）

| 评估项 | 结论 |
|---|---|
| **现在是否应该拆微服务？** | ❌ **不建议现在拆**。当前体量小、团队小、领域边界尚未完全稳定，拆分收益小于运维/调试/一致性成本。 |
| **现在架构能否保证未来无痛拆分？** | ⚠️ **不能**。当前是"按技术分层"的 MVC 结构（controller/model/storage 横切所有领域），领域边界散落在多个目录里，未来拆服务需要"先重新切领域、再切服务"两步大手术。 |
| **该怎么做？** | ✅ **保持单部署单元，但把"技术分层"重构为"领域优先（Modular Monolith）"**。让领域边界 = 包边界，单体内即模拟微服务，未来拆分只是"把某个领域包整体搬出去成一个服务"。 |

核心判断依据（为什么现在不该拆）见 §2；未来无痛拆分的关键障碍见 §3；改良方案见 §4–§7。

---

## 二、现在是否应该拆微服务？—— 不建议

### 2.1 微服务的收益与成本对照

| 维度 | 微服务收益 | 你当前是否真的需要 |
|---|---|---|
| 独立伸缩（如搜索流量大） | ✅ 真实收益 | ES/Redis/Redpanda 已独立伸缩，应用层各领域 QPS 量级相近，单体内也可按 handler 拆池化 |
| 故障隔离 | ✅ 真实收益 | 当前无明显"热点领域"拖垮全局的故障记录 |
| 团队并行开发（康威定律） | ✅ 真实收益 | 团队规模小，单仓单服务协作成本更低 |
| 技术栈异构 | ✅ 真实收益 | 全部是 Go，无异构需求 |
| **新增运维成本** | — | ❌ 需引入：服务注册发现、跨服务 tracing、分布式事务/最终一致性、API 网关、多套 CI/CD、多份监控仪表盘 |
| **新增调试成本** | — | ❌ 一次点赞链路（post → like → redpanda → redis）目前是进程内调用，拆后变 3 次跨网络调用 + 日志关联 |
| **新增数据一致性成本** | — | ❌ 当前 `CreateCircle` 用本地事务保证 circle + circle_member 原子性；拆服务后需 Saga / Outbox |

### 2.2 量化信号：你尚未达到"该拆"的临界点

行业里通常用以下信号判断拆分时机，逐条对照：

- **代码量**：当前业务代码约 1.3 万行，模型层 8 个聚合。**远低于**业界常见的"单服务 5–10 万行才考虑拆"的经验阈值。
- **部署频率差异**：各领域发布节奏一致（都是同一个 `cmd/apps/server.go`），无"想单独发版"的领域。
- **数据所有权**：`domains.circle` / `domains.post` 等表共用同一套 PostgreSQL 连接，无独立数据源需求。
- **性能瓶颈**：热点是 ES 检索和 Redis 计数，这些中间件已经独立部署，**应用层没有需要独立伸缩的子领域**。
- **团队结构**：单人/小团队，没有"按服务分团队"的组织约束。

> **结论**：现在拆微服务属于"过度工程化"，会引入与业务收益不匹配的复杂性。**正确做法是先把单体做对（领域清晰），等出现真实的拆分信号（团队分裂、某领域独立伸缩、某领域发布节奏解耦）时再拆，成本可控。**

---

## 三、未来要"无痛拆分"，当前架构的主要障碍

当前结构（README 里的标准三段式）：

```
pkg/server/
├── controller/      ← 所有领域的 HTTP handler 挤在一起
├── model/           ← 所有领域的实体 + DAO 挤在一起
├── storage/         ← redis/pgsql/elasticsearch/redpanda/s3 按技术切分
│   ├── redis/cache.go      (20KB，圈子统计+帖子统计+点赞+评论统计 全混在一起)
│   └── elasticsearch/      (circle.go/post.go/user.go 按领域切，但和 model/controller 不对齐)
└── router/          ← 所有路由集中注册
```

### 3.1 障碍一：领域边界 ≠ 包边界（核心问题）

"圈子"相关的代码今天分散在 7 个地方：
- `controller/circle.go`（HTTP 入口）
- `controller/post.go` 里 `GetCirclePosts`（帖子列表，但其实是"圈内帖子"领域）
- `model/circle.go` + `model/circle_member.go`（数据）
- `storage/redis/cache.go` 里的 `CircleStatistics` / `IncrementCircleMemberCount`（缓存）
- `storage/elasticsearch/circle.go`（检索）
- `storage/redpanda/` 里的 `PublishCircleMemberCount`（消息）
- `router/routers.go` 里 circle 路由块

**未来要拆"圈子服务"，意味着要在 7 个目录里各挑一部分代码搬走** —— 这正是单体演进到微服务最痛的环节。理想状态应该是"圈子相关的所有代码在一个包里，整体打包即可搬走"。

### 3.2 障碍二：领域间存在隐性耦合（跨领域直接 import）

扫描发现：
- `controller/like.go` 同时操作 `model.Comment`、`model.Post`、`model.CommentLike`、`model.PostLike`，并直接调用 `pgsql.DB` —— 一个点赞动作跨了 **点赞 + 帖子 + 评论** 三个领域。
- `controller/post.go` 的 `GetPosts` 里同时查 `model.GetUsersByIDs`（用户域）、`model.GetCirclesByIDs`（圈子域）、`model.GetPostsMediaByIDs`（帖子域）—— 一次列表组装跨 3 个领域。
- `controller/circle.go` 里的 `restoreAllCounters` 直接读 `model.GetCircleByID` 并写 Redis，把"缓存恢复"这种本应属于存储适配层的逻辑塞进了 HTTP controller。

**这类跨领域调用目前是"进程内函数调用"，拆服务后必须改成"RPC/事件"**。如果现在不收口，拆服务时这些调用点会全部爆开。

### 3.3 障碍三：model 层同时承担"实体定义 + 数据访问"

`model/circle.go` 里既有 `type Circle struct`（领域实体），又有 `func GetCircleByID(db *gorm.DB, circleID)`（DAO）。这导致：
- 领域实体的字段/行为与持久化细节耦合；
- 未来若 circle 服务改用 MongoDB 或分库分表，要动 `model` 包，而这个包被全项目共享。

### 3.4 障碍四：全局单例耦合（`pgsql.DB` / `redispkg` 包级变量）

`pgsql.DB` 是包级全局变量，每个 controller 直接 `pgsql.DB.Where(...)`。拆服务时，某个服务要换数据源、加读写分离、加分库路由，**没有任何抽象层可以切入**。

### 3.5 障碍五：router 是一个大函数，路由按领域分组但不按领域归属

`router/routers.go` 的 `RegisterRoutes` 是一个 117 行的大函数，把所有领域的路由平铺。没有"每个领域自己注册自己的路由"的能力，拆服务时路由表无法整体迁移。

---

## 四、改良方案总览：Modular Monolith（模块化单体）

### 4.1 设计目标

1. **领域边界 = 包边界**：每个领域的代码（HTTP、业务逻辑、持久化、缓存、事件）都在同一个子包内。
2. **领域间只能通过显式契约通信**：禁止跨领域直接 import 对方的 model/DAO，只能调用对方暴露的 Service 接口（进程内调用）。
3. **依赖方向单向**：领域 → 共享内核（shared kernel），领域之间通过接口反解。
4. **未来拆服务时**：把某个领域包整体抽出去 → 包内 Service 接口的实现从"本地调用"替换为"HTTP/gRPC client"→ 完事。**不需要动业务代码。**

### 4.2 目标目录结构

```
pkg/
├── shared/                          ← 共享内核（跨领域复用的基础设施）
│   ├── kernel/                      ← 值对象、基础错误、ID 类型（UUIDv7 封装）
│   ├── appctx/                      ← 请求上下文（登录用户 ID、设备、traceID）
│   ├── httputil/                    ← 统一响应（现 response 包）、绑定、错误映射
│   ├── persistence/                 ← DB/Redis/ES/S3/Redpanda 连接管理（不含业务）
│   └── platform/                    ← sa-token、oauth、logger、conf、邮件 等
│
├── domains/                         ← 业务领域（每个子包是一个"准微服务"）
│   ├── user/                        ← 用户域
│   │   ├── domain/                  ← 实体（SysUser）、值对象、领域常量、领域事件
│   │   │   ├── user.go
│   │   │   └── repository.go        ← interface UserRepository（领域定义接口）
│   │   ├── application/             ← 应用服务（编排，对应现 controller 的业务部分）
│   │   │   ├── service.go           ← UserService 接口 + 实现
│   │   │   └── dto.go               ← 入参/出参 DTO（UpdateProfileCmd 等）
│   │   ├── infrastructure/          ← 接口实现（GORM、Redis 缓存、ES 索引）
│   │   │   ├── user_repo_pg.go      ← 实现 UserRepository
│   │   │   └── user_search_es.go
│   │   └── interfaces/              ← 对外暴露
│   │       ├── http/                ← gin/hertz handler + 路由注册
│   │       │   ├── handler.go
│   │       │   └── routes.go        ← RegisterUserRoutes(rg)
│   │       └── api/                 ← （未来）给其他领域调用的 client 接口
│   │
│   ├── circle/                      ← 兴趣圈域（同上结构）
│   ├── post/                        ← 帖子域
│   ├── comment/                     ← 评论域
│   ├── like/                        ← 点赞域（独立成领域，不要散落在 post/comment）
│   ├── category/                    ← 分类域
│   ├── auth/                        ← 认证域（OAuth + 注册验证 + 登录）
│   └── storage/                     ← 文件上传域（S3）
│
└── composition/                     ← 组装层（单体内"模拟微服务编排"）
    ├── wire.go                      ← 依赖注入：实例化各领域 Service，注入 repo
    └── server.go                    ← 启动 HTTP server，挂载各领域 routes
```

### 4.3 领域划分清单（基于现有代码归纳）

| 领域 | 聚合根 | 当前散落位置 | 备注 |
|---|---|---|---|
| **user** | `SysUser` | model/user.go, controller/user.go, elasticsearch/user.go | 用户资料、搜索 |
| **auth** | （无聚合根，编排域） | controller/{login,register,oauth}.go, auth/* | OAuth + 邮箱注册 + sa-token 登录会话 |
| **circle** | `Circle` | model/circle.go, model/circle_member.go, controller/circle.go, redis/cache.go(部分), elasticsearch/circle.go | 圈子 + 成员 + 统计计数 |
| **post** | `Post` | model/post.go, controller/post.go, elasticsearch/post.go, redpanda/post 统计 | 帖子 + 帖子统计 |
| **comment** | `Comment` | model/comment.go, model/comment_like.go, controller/comment.go | 评论 + 回复 |
| **like** | `PostLike` / `CommentLike` | controller/like.go, model/{post,comment}_like.go, redis/like_lua.go | 点赞（横跨 post/comment，但逻辑独立，应独立成域） |
| **category** | `Category` | model/category.go, controller/category.go | 字典性质，最简单 |
| **storage** | （无聚合根） | controller/upload.go, storage/s3 | 文件上传 |

> **关键决策：把"点赞"从 post/comment 里独立出来**。当前 `controller/like.go` 已经是一个横切领域（同时处理 post 点赞和 comment 点赞），未来也很可能是独立服务（计数 + 通知），独立成 `domains/like` 最符合演进路线。

---

## 五、分层职责与依赖规则

### 5.1 四层架构（每个领域内）

```
interfaces (HTTP/RPC)   ←  入站适配器：解析请求、调用 application、组装响应
        ↓
application (Service)   ←  用例编排：事务边界、调用 domain + 其他领域 Service（通过接口）
        ↓
domain (Entity/Repo接口) ←  纯领域模型：无外部依赖，定义 Repository 接口、领域事件
        ↓
infrastructure (实现)    ←  出站适配器：GORM/Redis/ES/Redpanda 实现 domain 定义的接口
```

**铁律（用 lint 工具强制）：**
1. `domain` 包**不能** import 任何 `infrastructure`、`application`、`interfaces` 包，也不能 import `gorm`、`redis`、`elasticsearch`。
2. `interfaces/http` 只能 import `application` 和 `shared`，**不能直接 import `infrastructure`**（不能在 handler 里写 `pgsql.DB.Where`）。
3. **领域之间禁止互相 import**：`domains/post` 不能 import `domains/circle`。跨领域调用走 application 层的接口（见 §6）。

### 5.2 跨领域通信（为未来 RPC 铺路）

当前痛点：`post` 域需要"根据用户 ID 批量取用户名"。改造后：

```go
// domains/post/application/service.go
type PostService interface {
    ListPosts(ctx context.Context, q ListQuery) (*ListResult, error)
}

type postServiceImpl struct {
    postRepo    domain.PostRepository
    userFacade  UserFacade        // ← 接口，由 user 域提供实现
    circleFacade CircleFacade
}
```

- `post` 域定义自己需要的**最小接口**（`UserFacade.GetByIds`），不依赖 `domains/user` 的任何具体类型。
- 组装层（`composition/wire.go`）把 `domains/user` 的 `UserFacadeImpl` 注入给 `post`。
- **未来拆服务**：把 `UserFacadeImpl` 换成 `UserFacadeHTTPClient`，签名不变，`post` 业务代码一行不改。

---

## 六、关键改造点（落地清单）

### 6.1 抽取领域 Repository 接口，下放基础设施实现

**现状**：
```go
// controller/circle.go
circle, err := model.GetCircleByID(pgsql.DB, circleID)   // controller 直接持有 DB
```

**改造后**：
```go
// domains/circle/domain/repository.go
type CircleRepository interface {
    GetByID(ctx context.Context, id uuid.UUID) (*Circle, error)
    Create(ctx context.Context, c *Circle) error
    // ...
}

// domains/circle/infrastructure/circle_repo_pg.go
type circleRepoPG struct { db *gorm.DB }
func (r *circleRepoPG) GetByID(ctx context.Context, id uuid.UUID) (*Circle, error) { ... }

// domains/circle/interfaces/http/handler.go
type Handler struct { svc CircleService }
func (h *Handler) GetDetail(c AppContext) {
    vo, err := h.svc.GetDetail(c, c.PathParam("id"))
    ...
}
```

### 6.2 统一应用上下文（AppContext）替代 `*gin.Context` 泄漏

**现状**：`response.Success(c *gin.Context, ...)`、`utils.GetUserIDFromRequest(c *gin.Context)` 把 Web 框架类型传到了业务层。

**改造**：在 `shared/appctx` 定义一个最小接口：
```go
type AppContext interface {
    Context() context.Context
    UserID() (uuid.UUID, bool)
    Device() string
    Bind(v any) error          // JSON/Form/Query 绑定
    PathParam(name string) string
    Header(name string) string
}
```
- handler 里把 `*gin.Context`（或迁移后的 `*hertz.RequestContext`）包成 `AppContext` 传给 service。
- **好处**：业务层不再依赖具体 Web 框架，也与重构二（gin→hertz）解耦。建议两个重构**先做这个抽象**。

### 6.3 把 Redis/ES 中"领域相关"的部分迁进各自领域

- `storage/redis/cache.go` 里 `CircleStatistics*` / `IncrementCircleMemberCount` → `domains/circle/infrastructure/cache_redis.go`
- `storage/redis/cache.go` 里 `Post*` 统计 / `BatchCheckPostLiked` → `domains/post/infrastructure/cache_redis.go`
- `storage/redis/like_lua.go` → `domains/like/infrastructure/like_lua.go`
- `storage/elasticsearch/circle.go` → `domains/circle/infrastructure/search_es.go`，以此类推。
- `storage/redis/` 只保留**通用工具**（如 `GetJSONCompressed`、连接池），不含任何领域 key。

### 6.4 把 Redpanda 事件按领域归属拆分

- `PublishCircleMemberCount` / `PublishCirclePostCount` → `domains/circle/infrastructure/event_producer.go`
- `PublishPostViewCount` 等 → `domains/post/infrastructure/event_producer.go`
- `PublishCommentLikeEvent` / `PublishPostLikeEvent` → `domains/like/infrastructure/event_producer.go`
- 消费者（`like_consumer.go` 等）同样按领域归属，但放在 `composition/`（因为消费者是跨领域编排的入口）。

### 6.5 路由按领域自注册

```go
// domains/circle/interfaces/http/routes.go
func RegisterRoutes(rg RouterGroup, svc CircleService, mw MiddlewareSet) {
    c := &Handler{svc: svc}
    g := rg.Group("/circle", mw.RequireLogin())
    g.GET("/list", c.List)
    g.POST("/create", c.Create)
    // ...
}

// composition/server.go
func RegisterAll(r Engine, deps *Deps) {
    userRouter.RegisterRoutes(r, deps.UserService, deps.MW)
    circleRouter.RegisterRoutes(r, deps.CircleService, deps.MW)
    // ...
}
```
**好处**：未来拆"圈子服务"，直接把 `domains/circle/interfaces/http` 整包搬走即可，路由定义不丢。

### 6.6 收口全局单例

- `pgsql.DB` 不再被业务代码直接引用，只被 `composition/wire.go` 用来构造各 Repository 实例后注入。
- `redispkg` 同理：连接池在 composition 层构造，领域层只接收自己需要的 client 子集。

---

## 七、实施步骤（分阶段，低风险）

> 原则：**每一步可独立合并、可独立回滚、不改变外部 API 行为**。强烈建议先做重构二（gin→hertz）的"AppContext 抽象"部分，再开始本方案的领域拆分，避免重复改 handler。

### 阶段 0：建立 shared 内核（1–2 天）
- 新建 `pkg/shared/{kernel,appctx,httputil,persistence,platform}`。
- 把 `pkg/conf`、`pkg/logger`、`pkg/util`、`pkg/enums`、`pkg/server/response`、`pkg/server/storage/{db,redis,s3,elasticsearch,redpanda}` 的"连接/工具"部分搬入 `shared`（保持原 API 兼容）。
- **此步不改业务代码路径**，只迁移基础设施。

### 阶段 1：抽取 AppContext + Repository 接口（2–3 天）
- 定义 `shared/appctx.AppContext`、`shared/httputil.AppEngine`（路由抽象）。
- 为每个领域在 `pkg/domains/<x>/domain/` 定义 `Repository` 接口（暂时由旧的 `model` 函数包装实现，保证编译通过）。

### 阶段 2：逐领域搬迁（每个领域 1–2 天，可并行）
建议顺序（由简到繁，建立信心）：
1. `category`（最简单，验证流水线）
2. `storage`（独立，无跨域依赖）
3. `user`（被很多领域依赖，先建好 Facade）
4. `circle`（含统计计数、成员，复杂度中等）
5. `post`（依赖 user/circle facade）
6. `comment`
7. `like`（横跨 post/comment，最后做）
8. `auth`（依赖 user，但流程独立）

每个领域搬迁完成后：
- 删除 `pkg/server/controller/`、`pkg/server/model/` 中对应文件；
- 更新 `composition/server.go` 装配；
- 跑接口冒烟测试，确认行为不变。

### 阶段 3：消除跨领域直接依赖（1–2 天）
- 全局搜索 `domains/<A>` 是否 import 了 `domains/<B>`，逐一改为 Facade 接口注入。
- 在 `composition/wire.go` 完成所有 Facade 的绑定。

### 阶段 4：加架构守护（0.5 天）
- 用 `go-arch-lint` 或 `depguard`（golangci-lint）写一条规则：`domains/*` 之间禁止互相依赖；`domain` 子包禁止 import `gorm`/`redis`/`elasticsearch`。
- 把规则接入 CI，防止未来回潮。

---

## 八、"未来拆服务"演练（验证方案有效性）

假设 18 个月后，"圈子"流量暴增，决定把 circle 拆成独立服务。在本方案下：

| 步骤 | 动作 | 改动范围 |
|---|---|---|
| 1 | 把 `pkg/domains/circle/` 整包复制到新仓库 | 0 行代码修改 |
| 2 | 在新仓库实现 `composition/server.go`，把 circle 的 HTTP 路由挂起来 | 新增 ~50 行 |
| 3 | 在主服务里，把注入给 `post`/`like` 的 `CircleFacadeImpl` 换成 `CircleFacadeHTTPClient` | 改 1 处装配代码 |
| 4 | 数据库层面，circle 表可以继续共享 PostgreSQL（按 schema 隔离），也可独立迁库 | 由 `domains.circle` schema 边界保证 |

**业务代码（handler/service/domain）零修改** —— 这就是"无痛拆分"。当前结构做不到这点，因为 circle 的代码散落在 7 个目录里。

---

## 九、风险与权衡

| 风险 | 缓解措施 |
|---|---|
| 重构期间引入 bug | 每阶段保留旧目录、灰度切换；坚持"行为不变"原则，先搬迁不改逻辑 |
| 工作量较大（约 2 周） | 分阶段提交，每阶段独立可发布；可与重构二（hertz 迁移）的 AppContext 抽象合并推进 |
| 团队不熟悉 DDD | 本方案刻意弱化 DDD 形式（无 Aggregate Root 复杂生命周期、无 CQRS），只保留"领域包 + 接口隔离"的核心，学习成本低 |
| 过度设计风险 | 已用"未拆服务"的结论对冲：现在只做包结构整理，不引入 RPC/消息总线的运行时复杂度 |

---

## 十、与重构二（gin→hertz）的关系

两个重构**高度协同**：
- 本方案的 `shared/appctx.AppContext` 抽象，**正好是 hertz 迁移的必要前置**（让业务层不绑定 gin）。
- 建议执行顺序：**先做 AppContext + Response 抽象 → 再做 hertz 迁移 → 最后做完整领域拆分**。这样 hertz 迁移只动 `interfaces/http` 层，不波及业务代码。
- 详见 `docs/refactor-2-gin-to-hertz-migration.md`。

---

## 附录 A：当前代码体量统计（供决策参考）

| 层 | 文件数 | 行数 |
|---|---|---|
| controller | 12 | 3,172 |
| model | 10 | 1,326 |
| storage（全） | 16 | ~5,000 |
| router/middleware | 6 | ~400 |
| 其它（conf/logger/util/auth） | ~10 | ~1,500 |
| **合计业务代码** | **~54** | **~11,400** |

> 体量数据支撑 §2.2 的判断：远未到"必须拆"的临界点，但已到"不重构就开始堆积技术债"的临界点。
