# 重构一：DDD 单体改良 —— 搬迁进度跟踪

> 最后更新：2026-06-15（批次 C 完成，所有领域搬迁完毕）

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
| **Facade 桥接器集合** | `pkg/composition/facade_bridges.go` | user→(circle,post,comment)、post→comment、post→like、comment→like 的字段级 Facade 适配器 |

## 已搬迁领域

| 领域 | 状态 | 路径 | 备注 |
|---|---|---|---|
| ✅ category | 完成 | `pkg/domains/category/` | 试点，验证流水线全通 |
| ✅ storage | 完成 | `pkg/domains/storage/` | 文件上传（S3），无跨域依赖 |
| ✅ user | 完成 | `pkg/domains/user/` | 含 UserFacade 接口（供 post/circle 等领域调用） |
| ✅ auth | 完成 | `pkg/domains/auth/` | login/register/oauth，通过 UserSessionStore 桥接 user 领域 |
| ✅ circle | 完成（批次B） | `pkg/domains/circle/` | 最大领域，含统计计数/成员/圈内帖子组装 |
| ✅ post | 完成（批次B） | `pkg/domains/post/` | 依赖 user/circle Facade，含点赞状态/浏览量异步累加 |
| ✅ comment | 完成（批次C） | `pkg/domains/comment/` | 树形结构 + 游标分页；依赖 user Facade + post 查询端口 |
| ✅ like | 完成（批次C） | `pkg/domains/like/` | 横跨 post/comment，Redis Lua 原子切换；依赖 post/comment 查询端口 |

## 待搬迁领域（批次 C）

> ✅ 全部完成。所有业务领域已搬迁到 `pkg/domains/`，无遗留待迁移领域。

## 关键设计决策（搬迁时遵循）

1. **领域四层结构**：domain（实体+接口）→ infrastructure（实现）→ application（service）→ interfaces/http（handler+routes）
2. **领域包禁止 import gin**：通过 AppContext + routing 抽象隔离
3. **跨领域通信用 Facade 接口**：定义在被调用方的 application 层，由 composition 注入实现
4. **鉴权用 composition.RequireLogin**：不用 sagin.CheckLogin（框架绑定）
5. **路由自注册**：每个领域 `interfaces/http/routes.go` 提供 `RegisterRoutes(rg, svc, mw)`
6. **BaseModel 共享内核**：`pkg/shared/domain/base.go`，所有领域实体内嵌（不再用 model.BaseModel）
7. **UserSessionStore 模式**：auth 领域需要读写用户数据时，通过 composition 层桥接器调用 user 领域，避免领域间直接依赖
8. **Facade 字段级桥接**：各领域定义自己的 Brief VO（如 `circle.application.UserBrief` vs `post.application.UserBrief`），结构相同但类型独立。composition 层的 `facade_bridges.go` 做字段级转换，保持领域间零类型耦合
9. **互注模式**：post 和 circle 互相依赖（post 需 CircleFacade/MemberChecker，circle 的 GetCirclePosts 需 PostMediaFetcher）。composition 先构造两个 Service，再通过 setter 互注 Facade，打破构造期循环
10. **横跨聚合的领域**：like 领域横跨 post + comment 两个聚合。它不持有独立聚合根表（PostLike/CommentLike 表分别属于 post/comment 领域），但统一管理"点赞/取消点赞"用例。通过 `PostTarget` / `CommentTarget` 端口接口分别依赖 post 和 comment 领域，在 composition 层桥接
11. **恢复型缓存端口**：comment/like 在写入统计缓存前需先确保 stats Hash 存在（否则 Redis Lua 脚本读到空 stats）。通过 `RestoreStats` / `RestoreStatsAndIncrCommentCount` 端口委托给 post/comment 领域，复用各自的 `restoreXxxStatsIfNeed` 逻辑（DB 回源 → 写 Redis）
12. **消费者解耦**：`pkg/server/storage/redpanda/like_consumer.go` 原本依赖 `model.CommentLike`/`model.PostLike`，批次 C 改为 import `comment.domain.CommentLike` / `post.domain.PostLike`，解除对 `pkg/server/model` 的依赖，使 model 包可瘦身

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
- `pkg/server/controller/circle.go`（→ `pkg/domains/circle/`，批次B）
- `pkg/server/controller/post.go`（→ `pkg/domains/post/`，批次B）
- `pkg/server/controller/comment.go`（→ `pkg/domains/comment/`，批次C）
- `pkg/server/controller/like.go`（→ `pkg/domains/like/`，批次C）
- `pkg/server/controller/controller.go`、`hello.go`（死代码，批次C 删除）
- `pkg/server/controller/` 整个目录（空目录删除，批次C）
- `pkg/server/router/routers.go`（所有领域已搬完，批次C 删除）
- `pkg/server/utils/sa_token_util.go`（死代码，批次C 删除）
- `pkg/server/model/comment.go`、`comment_like.go`、`post.go`、`post_like.go`、`circle.go`、`circle_member.go`、`hello.go`（已迁移到各领域，批次C 删除）

## 待清理（批次 C 后评估）

- `pkg/server/model/user.go`（仅剩 `SysUser` 实体，仍被 `pkg/server/auth/*` OAuth provider 和 `pgsql/connect.go` AutoMigrate 使用。待 auth 领域内联 OAuth provider 后可整体删除）
- `pkg/server/model/base.go`（BaseModel 别名存根，指向 `pkg/shared/domain/base.go`。随 `model/user.go` 一起删除）
- `pkg/server/auth/`（OAuth provider 适配器已在新架构中包装，旧代码待后续 refactor 评估是否内联到 `pkg/domains/auth/infrastructure/`）
- ~~`pkg/server/response/`~~ ✅ **已在重构二（gin→hertz 迁移，2026-06-16）删除**（连同死代码 `pkg/server/router/middleware/csrf.go`、`cache.go`，业务侧早已改用 `pkg/shared/httputil`）
