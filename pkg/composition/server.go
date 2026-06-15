// Package composition 的 server.go：装配各领域依赖并注册路由。
//
// 本文件目前只接入 category 领域（试点）。其余领域仍走旧的
// pkg/server/router/routers.go 注册路径。待各领域陆续搬迁完成后，
// 所有路由注册会收口到本文件，届时 routers.go 将被删除。
package composition

import (
	"interestBar/pkg/composition/ginadapter"
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	"interestBar/pkg/shared/routing"

	"github.com/gin-gonic/gin"
)

// RegisterDomainRoutes 把所有"已搬迁到 domains/"的领域路由挂到 gin engine 上。
//
// 注意：当前仍是 gin 框架，但通过 ginadapter 包装后，领域包感知不到 gin。
// 未来迁移 hertz 时，只需把 ginadapter 换成 hertzadapter，本函数签名不变。
func RegisterDomainRoutes(e *gin.Engine) {
	deps := NewDeps()
	root := ginadapter.ForEngine(e)
	authCheck := RequireLogin // 见 auth.go（框架无关的鉴权中间件）

	// 装配并注册各领域
	registerCategory(root, deps, authCheck)

	// 后续领域搬迁后，在此处依次追加：
	// registerUser(root, deps, authCheck)
	// registerCircle(root, deps, authCheck)
	// ...
}

// registerCategory 装配 category 领域的依赖并注册路由。
//
// 依赖链：
//
//	pgsql.DB → CategoryRepository（infra）
//	                 ↓
//	         Redis client → CategoryCache（infra）
//	                 ↓
//	            CategoryService（application）
//	                 ↓
//	            Handler（interfaces/http）
func registerCategory(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	repo := categoryinfra.NewCategoryRepository(deps.DB.Get())
	cache := categoryinfra.NewCategoryCache()
	svc := categoryapp.NewCategoryService(repo, cache)
	categoryhttp.RegisterRoutes(root, svc, authCheck)
}
