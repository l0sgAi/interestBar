# references/layers.md — DDD 四层代码模板

> 本文档从已完成的 `category` 领域提取真实代码作为模板。
> 新建领域或新增 API 时，照此结构写。`<domain>` 替换为领域名（如 `circle`、`post`）。

## 目录骨架

```
pkg/domains/<domain>/
├── domain/
│   ├── <domain>.go          实体 + 常量
│   └── repository.go        Repository/Cache 接口
├── application/
│   └── service.go           Service 接口 + 实现 + VO/DTO
├── infrastructure/
│   ├── <domain>_repo_pg.go  GORM 实现
│   └── <domain>_cache_redis.go  Redis 实现（可选）
└── interfaces/
    └── http/
        ├── handler.go       handler（接收 AppContext）
        └── routes.go        路由自注册
```

---

## domain 层

### 实体（`domain/<domain>.go`）

```go
// Package domain 存放 <domain> 领域的纯领域模型。
// 依赖规则：不得 import gorm/redis/es 或其他领域包。
package domain

import (
	"time"
	"github.com/google/uuid"
)

// <Entity> 聚合根。字段与数据库表对齐。
type Circle struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Name        string    `json:"name" gorm:"column:name;type:varchar(50);not null"`
	// ... 其它字段
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指向 domains.<table> schema。
func (Circle) TableName() string { return "domains.circle" }

// 领域常量
const (
	CircleStatusNormal = 1
	CircleStatusBanned = 2
)
```

**规则：**
- 实体内嵌时间戳字段（不用 BaseModel 内嵌也行，但字段要对齐）。
- `TableName` 永远指向 `domains.<table>`（与数据库 schema 一致）。
- 常量定义在本文件，不散落到其它层。

### Repository 接口（`domain/repository.go`）

```go
package domain

import (
	"context"
	"github.com/google/uuid"
)

// <Entity>Repository 持久化接口，由 infrastructure 实现。
type CircleRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Circle, error)
	Create(ctx context.Context, c *Circle) error
	// ... 按需添加方法
}

// <Entity>Cache 缓存接口（可选）。
type CircleCache interface {
	Get(ctx context.Context, id uuid.UUID) (*Circle, error) // 未命中返回 nil,nil
	Set(ctx context.Context, c *Circle) error
}
```

**规则：**
- 接口定义在 domain 层，实现在 infrastructure 层（依赖倒置）。
- 所有方法第一个参数是 `context.Context`。
- Cache 的 Get 在未命中时返回 `nil, nil`（不是 error），让 application 层据此回源。

---

## application 层（`application/service.go`）

```go
// Package application 提供 <domain> 领域的应用服务。
package application

import (
	"context"
	"interestBar/pkg/domains/<domain>/domain"
)

// <Entity>SimpleVO 返回给 HTTP 层的视图对象。
type CircleSimpleVO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// <Entity>Service 应用服务接口。
type CircleService interface {
	GetByID(ctx context.Context, id string) (*CircleSimpleVO, error)
}

type circleServiceImpl struct {
	repo  domain.CircleRepository
	cache domain.CircleCache
}

// New<Entity>Service 构造函数，接收 domain 接口（不接收具体实现）。
func NewCircleService(repo domain.CircleRepository, cache domain.CircleCache) CircleService {
	return &circleServiceImpl{repo: repo, cache: cache}
}

// GetByID 用例编排：先查缓存，miss 回源 DB。
func (s *circleServiceImpl) GetByID(ctx context.Context, id string) (*CircleSimpleVO, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrInvalidID
	}

	// 1. 查缓存
	cached, _ := s.cache.Get(ctx, uid)
	if cached != nil {
		return toVO(cached), nil
	}

	// 2. 回源 DB
	circle, err := s.repo.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	// 3. 回写缓存（失败不影响主流程）
	_ = s.cache.Set(ctx, circle)

	return toVO(circle), nil
}
```

**规则：**
- Service 定义为**接口**（便于 composition 注入不同实现、便于测试 mock）。
- 构造函数参数是 domain 接口类型，不写 `*gorm.DB` / `*redis.Client`。
- 返回 VO/DTO，不直接返回 domain 实体（避免数据库细节泄漏到 HTTP 层）。
- ID 在 VO 里用 `string`（JSON 友好），在 domain 里用 `uuid.UUID`。

---

## infrastructure 层

### Repository 实现（`infrastructure/<domain>_repo_pg.go`）

```go
// Package infrastructure 提供 <domain> 领域的基础设施实现。
package infrastructure

import (
	"context"
	"interestBar/pkg/domains/<domain>/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type circleRepoPG struct {
	db *gorm.DB
}

// New<Entity>Repository 构造基于 PostgreSQL 的实现。
func NewCircleRepository(db *gorm.DB) domain.CircleRepository {
	return &circleRepoPG{db: db}
}

func (r *circleRepoPG) GetByID(ctx context.Context, id uuid.UUID) (*domain.Circle, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}
```

**规则：**
- 实现类持有 `*gorm.DB`（构造函数注入）。
- 查询时用 `.WithContext(ctx)` 透传 context。
- 返回类型是 `domain.Circle`（领域的类型），不是本地新定义的类型。

### Cache 实现（`infrastructure/<domain>_cache_redis.go`）

```go
package infrastructure

import (
	"context"
	"time"
	"interestBar/pkg/domains/<domain>/domain"
	redispkg "interestBar/pkg/server/storage/redis"
)

const (
	circleCacheKeyPrefix = "circle:info:"
	circleCacheTTL       = 30 * time.Minute
)

type circleCacheRedis struct{}

func NewCircleCache() domain.CircleCache {
	return &circleCacheRedis{}
}

func (c *circleCacheRedis) Get(ctx context.Context, id uuid.UUID) (*domain.Circle, error) {
	var circle domain.Circle
	err := redispkg.GetJSON(circleCacheKeyPrefix+id.String(), &circle)
	if err != nil {
		return nil, nil // 未命中返回 nil,nil
	}
	return &circle, nil
}
```

**规则：**
- 复用 `pkg/server/storage/redis` 的工具函数（`GetJSON`/`SetJSON`），不直接持有 `*redis.Client`。
- 过渡期允许 import `pkg/server/storage/redis`（这是尚未搬迁的共享存储工具）。

---

## interfaces/http 层

### handler（`interfaces/http/handler.go`）

```go
// Package http 提供 <domain> 领域的 HTTP 入站适配器。
package http

import (
	"interestBar/pkg/domains/<domain>/application"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

type Handler struct {
	svc application.CircleService
}

func NewHandler(svc application.CircleService) *Handler {
	return &Handler{svc: svc}
}

// GetByID GET /circle/detail/:id
// 接收 appctx.AppContext（不是 *gin.Context）
func (h *Handler) GetByID(c appctx.AppContext) {
	id := c.Param("id")
	if id == "" {
		httputil.BadRequest(c, "id is required")
		return
	}

	vo, err := h.svc.GetByID(c, id)
	if err != nil {
		httputil.InternalError(c, "Failed to get circle")
		return
	}
	httputil.Success(c, vo)
}
```

**规则：**
- handler 方法签名固定为 `func (h *Handler) Xxx(c appctx.AppContext)`。
- 参数绑定用 `c.BindJSON(&req)` / `c.BindQuery(&req)` / `c.Param("id")`。
- 响应一律用 `httputil.Success` / `httputil.BadRequest` 等，**不要**用 `c.JSON` 直接写（除非有特殊状态码需求）。
- handler 不写业务逻辑（不调用 Repository），只调 Service。

### 路由注册（`interfaces/http/routes.go`）

```go
package http

import (
	"interestBar/pkg/domains/<domain>/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 <domain> 领域路由挂到路由组上。
// authCheck 由 composition 注入（composition.RequireLogin）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CircleService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// /circle 组：所有接口都需要登录
	g := rg.Group("/circle", authCheck)
	{
		g.GET("/detail/:id", h.GetByID)
		g.POST("/create", h.Create)
	}
}
```

**规则：**
- 每个领域自己注册路由，不在 composition 里写路径。
- 需要鉴权的组，把 `authCheck` 作为 `Group` 的中间件传入。
- 公开路由（不需要登录）不传 `authCheck`。

---

## composition 装配（`pkg/composition/server.go` 追加）

```go
func registerCircle(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	repo := circleinfra.NewCircleRepository(deps.DB.Get())
	cache := circleinfra.NewCircleCache()
	svc := circleapp.NewCircleService(repo, cache)
	circlehttp.RegisterRoutes(root, svc, authCheck)
}
```

然后在 `RegisterDomainRoutes` 里调用 `registerCircle(root, deps, authCheck)`。

---

## AppContext 常用方法速查

| 方法 | 用途 |
|---|---|
| `c.Param("id")` | 路径参数 `:id` |
| `c.Query("k")` | URL query |
| `c.Header("k")` | 请求头 |
| `c.BindJSON(&req)` | 绑定 JSON body + validator 校验 |
| `c.BindQuery(&req)` | 绑定 query（`form` tag） |
| `c.UserID()` | 当前登录用户 ID（uuid.UUID, bool） |
| `c.LoginID()` | 当前 loginID（string, bool） |

响应用 `httputil`：`Success` / `SuccessWithMessage` / `BadRequest` / `Unauthorized` / `Forbidden` / `NotFound` / `InternalError` / `Conflict`。错误响应会自动调用 `c.Abort()`。
