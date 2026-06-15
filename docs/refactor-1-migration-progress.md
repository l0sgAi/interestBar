# 重构一：DDD 单体改良 —— 搬迁进度跟踪

> 最后更新：2026-06-15

## 已完成的基建（可复用）

| 组件 | 路径 | 说明 |
|---|---|---|
| AppContext 抽象 | `pkg/shared/appctx/context.go` | 框架无关的请求上下文接口 |
| gin 版 AppContext | `pkg/shared/appctx/ginadapter/adapter.go` | gin→AppContext 适配（过渡期） |
| httputil 响应工具 | `pkg/shared/httputil/response.go` | 框架无关版 response（替代旧 pkg/server/response） |
| routing 路由抽象 | `pkg/shared/routing/group.go` | RouterGroup + HandlerFunc 抽象 |
| composition 组装层 | `pkg/composition/` | 依赖注入 + 路由编排 |
| gin 版 RouterGroup | `pkg/composition/ginadapter/group.go` | gin→routing 适配 |
| 框架无关鉴权 | `pkg/composition/auth.go` | RequireLogin（stputil 直校验，替代 sagin.CheckLogin） |
| DBHolder | `pkg/server/storage/db/pgsql/connect.go` | DB 依赖注入 holder |

## 已搬迁领域

| 领域 | 状态 | 路径 | 备注 |
|---|---|---|---|
| ✅ category | 完成 | `pkg/domains/category/` | 试点，验证流水线全通 |

## 待搬迁领域（建议顺序）

| 领域 | 依赖 | 预计工时 | 备注 |
|---|---|---|---|
| ⬜ storage | 无 | 0.5 天 | 文件上传，独立 |
| ⬜ user | 被 post/comment/like 依赖 | 0.5 天 | 先建 Facade |
| ⬜ circle | user facade | 1 天 | 含统计计数/成员 |
| ⬜ post | user/circle facade | 1 天 | 依赖较多 |
| ⬜ comment | user/post facade | 0.5 天 | |
| ⬜ like | post/comment facade | 0.5 天 | 横跨 post/comment |
| ⬜ auth | user | 0.5 天 | OAuth+注册+登录 |

## 关键设计决策（搬迁时遵循）

1. **领域四层结构**：domain（实体+接口）→ infrastructure（实现）→ application（service）→ interfaces/http（handler+routes）
2. **领域包禁止 import gin**：通过 AppContext + routing 抽象隔离
3. **跨领域通信用 Facade 接口**：定义在被调用方的 application 层，由 composition 注入实现
4. **鉴权用 composition.RequireLogin**：不用 sagin.CheckLogin（框架绑定）
5. **路由自注册**：每个领域 `interfaces/http/routes.go` 提供 `RegisterRoutes(rg, svc, mw)`

## 架构守护（已验证）

- ✅ `domains/` 下无任何 gin/hertz import
- ✅ 领域之间无跨领域依赖
- ✅ domain 层只依赖标准库
- ✅ `go build ./...` 通过
- ✅ `go vet ./...` 通过
