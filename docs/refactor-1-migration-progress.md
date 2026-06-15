# 重构一：DDD 单体改良 —— 搬迁进度跟踪

> 最后更新：2026-06-15

## 已完成的基建（可复用）

| 组件 | 路径 | 说明 |
|---|---|---|
| AppContext 抽象 | `pkg/shared/appctx/context.go` | 框架无关的请求上下文接口（含 FormFile/MultipartForm/Redirect） |
| gin 版 AppContext | `pkg/shared/appctx/ginadapter/adapter.go` | gin→AppContext 适配（过渡期） |
| httputil 响应工具 | `pkg/shared/httputil/response.go` | 框架无关版 response（替代旧 pkg/server/response） |
| routing 路由抽象 | `pkg/shared/routing/group.go` | RouterGroup + HandlerFunc 抽象 |
| **BaseModel 共享内核** | `pkg/shared/domain/base.go` | UUIDv7 主键 + 时间戳，所有领域实体内嵌 |
| composition 组装层 | `pkg/composition/` | 依赖注入 + 路由编排 + 跨领域桥接器 |
| gin 版 RouterGroup | `pkg/composition/ginadapter/group.go` | gin→routing 适配 |
| 框架无关鉴权 | `pkg/composition/auth.go` | RequireLogin（stputil 直校验，替代 sagin.CheckLogin） |
| DBHolder | `pkg/server/storage/db/pgsql/connect.go` | DB 依赖注入 holder |
| **UserSessionStore 桥接器** | `pkg/composition/user_session_store_bridge.go` | auth↔user 跨领域通信的关键适配器 |

## 已搬迁领域

| 领域 | 状态 | 路径 | 备注 |
|---|---|---|---|
| ✅ category | 完成 | `pkg/domains/category/` | 试点，验证流水线全通 |
| ✅ storage | 完成 | `pkg/domains/storage/` | 文件上传（S3），无跨域依赖 |
| ✅ user | 完成 | `pkg/domains/user/` | 含 UserFacade 接口（供 post/circle 等领域调用） |
| ✅ auth | 完成 | `pkg/domains/auth/` | login/register/oauth，通过 UserSessionStore 桥接 user 领域 |

## 待搬迁领域（建议顺序）

| 领域 | 依赖 | 预计工时 | 备注 |
|---|---|---|---|
| ⬜ circle | user facade | 1 天 | 含统计计数/成员，最大领域（860 行 controller） |
| ⬜ post | user/circle facade | 1 天 | 依赖较多，含点赞状态查询 |
| ⬜ comment | user/post facade | 0.5 天 | 树形结构 + 游标分页 |
| ⬜ like | post/comment facade | 0.5 天 | 横跨 post/comment，Redis lua |

## 关键设计决策（搬迁时遵循）

1. **领域四层结构**：domain（实体+接口）→ infrastructure（实现）→ application（service）→ interfaces/http（handler+routes）
2. **领域包禁止 import gin**：通过 AppContext + routing 抽象隔离
3. **跨领域通信用 Facade 接口**：定义在被调用方的 application 层，由 composition 注入实现
4. **鉴权用 composition.RequireLogin**：不用 sagin.CheckLogin（框架绑定）
5. **路由自注册**：每个领域 `interfaces/http/routes.go` 提供 `RegisterRoutes(rg, svc, mw)`
6. **BaseModel 共享内核**：`pkg/shared/domain/base.go`，所有领域实体内嵌（不再用 model.BaseModel）
7. **UserSessionStore 模式**：auth 领域需要读写用户数据时，通过 composition 层桥接器调用 user 领域，避免领域间直接依赖

## 行为变更说明

- `/auth/me` 路由合并到 `/user/get`（两者原本调用同一个 handler，返回相同的会话用户数据）。前端如有依赖 `/auth/me`，改为 `/user/get` 即可。
- `/upload/post-images`、`/upload/video`、`/upload/delete`、`/upload/presign` 这四个接口原本在 `upload.go` 中定义但未在 routers.go 注册。迁移后一并在 `/upload` 组下挂出（"代码即接口"原则）。

## 架构守护（已验证）

- ✅ `domains/` 下无任何 gin/hertz import
- ✅ `domains/` 下无任何 sa-token-go/integrations import
- ✅ 领域之间无跨领域依赖（auth 不 import user，user 不 import auth）
- ✅ domain 层只依赖标准库 + uuid
- ✅ `go build ./...` 通过
- ✅ `go vet ./...` 通过
- ✅ `go test ./...` 通过

## 已删除的旧文件

- `pkg/server/controller/upload.go`（→ `pkg/domains/storage/`）
- `pkg/server/controller/user.go`（→ `pkg/domains/user/`）
- `pkg/server/controller/login.go`（→ `pkg/domains/auth/`）
- `pkg/server/controller/register.go`（→ `pkg/domains/auth/`）
- `pkg/server/controller/oauth.go`（→ `pkg/domains/auth/`）

## 待清理（批次 B/C 完成后）

- `pkg/server/controller/`（circle/post/comment/like 搬完后整个删除）
- `pkg/server/model/`（所有领域搬完后删除，BaseModel 已迁至 shared kernel）
- `pkg/server/router/routers.go`（所有领域搬完后删除）
- `pkg/server/utils/sa_token_util.go`（circle/post/comment/like 搬完后删除）
- `pkg/server/auth/`（OAuth provider 适配器已在新架构中包装，旧代码待批次 B/C 后评估是否内联）
