// Package composition 的 server.go：装配各领域依赖并注册路由。
//
// 所有"已搬迁到 domains/"的领域路由都通过本文件挂到 gin engine 上。
// 旧路由（pkg/server/router/routers.go）保留尚未搬迁的领域；
// 待所有领域搬迁完成后，routers.go 将被删除，本文件成为唯一注册入口。
package composition

import (
	"interestBar/pkg/composition/ginadapter"
	authapp "interestBar/pkg/domains/auth/application"
	authhttp "interestBar/pkg/domains/auth/interfaces/http"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	storageapp "interestBar/pkg/domains/storage/application"
	storagehttp "interestBar/pkg/domains/storage/interfaces/http"
	storageinfra "interestBar/pkg/domains/storage/infrastructure"
	userapp "interestBar/pkg/domains/user/application"
	userhttp "interestBar/pkg/domains/user/interfaces/http"
	userinfra "interestBar/pkg/domains/user/infrastructure"
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
	registerStorage(root, deps, authCheck)
	registerUser(root, deps, authCheck)
	registerAuth(root, deps, authCheck)

	// 后续领域搬迁后，在此处依次追加：
	// registerCircle(root, deps, authCheck)
	// registerPost(root, deps, authCheck)
	// ...
}

// registerCategory 装配 category 领域的依赖并注册路由。
func registerCategory(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	repo := categoryinfra.NewCategoryRepository(deps.DB.Get())
	cache := categoryinfra.NewCategoryCache()
	svc := categoryapp.NewCategoryService(repo, cache)
	categoryhttp.RegisterRoutes(root, svc, authCheck)
}

// registerStorage 装配 storage 领域的依赖并注册路由。
//
// 依赖链：
//
//	S3 client（全局单例） → ObjectStorage（infra）
//	                          ↓
//	                    StorageService（application）
//	                          ↓
//	                    Handler（interfaces/http）
func registerStorage(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	storage := storageinfra.NewObjectStorage()
	svc := storageapp.NewStorageService(storage)
	storagehttp.RegisterRoutes(root, svc, authCheck)
}

// registerUser 装配 user 领域的依赖并注册路由。
//
// 依赖链：
//
//	pgsql.DB → UserRepository（infra）
//	             ↓
//	     Redis client → UserCache（infra）
//	             ↓
//	     ES client → UserSearcher（infra）
//	             ↓
//	     UserService（application）
//	             ↓
//	     Handler（interfaces/http）
//
// 注意：UserService 同时被 registerAuth 复用（构造 UserSessionStore 桥接器）。
// 这里不直接持有 svc 实例，而是每次需要时重新构造——过渡期可接受，
// 后续可以把所有 service 实例收口到 Deps 里统一管理。
func registerUser(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	svc := newUserService(deps)
	userhttp.RegisterRoutes(root, svc, authCheck)
}

// newUserService 构造一个 UserService，供 registerUser 和 registerAuth 复用。
func newUserService(deps *Deps) userapp.UserService {
	repo := userinfra.NewUserRepository(deps.DB.Get())
	cache := userinfra.NewUserCache()
	searcher := userinfra.NewUserSearcher()
	return userapp.NewUserService(repo, cache, searcher)
}

// registerAuth 装配 auth 领域的依赖并注册路由。
//
// 依赖链：
//
//	stputil → SaTokenSession（infra）
//	             ↓
//	pgsql.DB → UserSessionStore 桥接器（composition 层，桥接到 user 领域）
//	             ↓
//	Redis client → VerificationStore（infra）
//	             ↓
//	email client → EmailSender（infra）
//	             ↓
//	pkg/server/auth → OAuthProviderRegistry（infra，适配旧 Provider）
//	             ↓
//	AuthService（application）
//	             ↓
//	Handler（interfaces/http）
func registerAuth(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	session := authinfra.NewSaTokenSession()
	userStore := NewUserSessionStore(deps.DB.Get())
	verify := authinfra.NewVerificationStore()
	email := authinfra.NewEmailSender()
	oauthReg := authinfra.NewOAuthProviderRegistry()

	svc := authapp.NewAuthService(session, userStore, verify, email, oauthReg)
	authhttp.RegisterRoutes(root, svc, authCheck)
}
