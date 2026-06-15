// Package composition 的 server.go：装配各领域依赖并注册路由。
package composition

import (
	"interestBar/pkg/composition/ginadapter"
	authapp "interestBar/pkg/domains/auth/application"
	authhttp "interestBar/pkg/domains/auth/interfaces/http"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circlehttp "interestBar/pkg/domains/circle/interfaces/http"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	postapp "interestBar/pkg/domains/post/application"
	posthttp "interestBar/pkg/domains/post/interfaces/http"
	postinfra "interestBar/pkg/domains/post/infrastructure"
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
func RegisterDomainRoutes(e *gin.Engine) {
	deps := NewDeps()
	root := ginadapter.ForEngine(e)
	authCheck := RequireLogin

	// 先构造各领域 Service（不立即注册路由，因为要互注 Facade）
	userSvc := newUserService(deps)
	userFacade := userapp.NewUserFacade(userSvc)

	circleSvc, circleRepo, memberRepo := newCircleService(deps)
	circleFacade := circleapp.NewCircleFacade(circleRepo)

	postSvc := newPostService(deps)

	// 互注跨领域 Facade
	// circle 需要 user Facade + post 媒体查询器
	circleSvc.SetUserFacade(&circleUserFacade{delegate: userFacade})
	circleSvc.SetPostFetcher(&postMediaFetcherForCircle{delegate: postSvc})

	// post 需要 user Facade + circle Facade + 成员/状态校验器 + 帖子计数端口
	postSvc.SetUserFacade(&postUserFacade{delegate: userFacade})
	postSvc.SetCircleFacade(&postCircleFacade{delegate: circleFacade})
	postSvc.SetMemberChecker(&circleMemberCheckerForPost{memberRepo: memberRepo})
	postSvc.SetStatusChecker(&circleStatusCheckerForPost{circleRepo: circleRepo})
	postSvc.SetPostCountPort(&circlePostCountPortForPost{svc: circleSvc})

	// 注册路由
	registerCategory(root, deps, authCheck)
	registerStorage(root, deps, authCheck)
	registerUser(root, userSvc, authCheck)
	registerAuth(root, deps, authCheck)
	registerCircle(root, circleSvc, authCheck)
	registerPost(root, postSvc, authCheck)
}

// registerCategory 装配 category 领域。
func registerCategory(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	repo := categoryinfra.NewCategoryRepository(deps.DB.Get())
	cache := categoryinfra.NewCategoryCache()
	svc := categoryapp.NewCategoryService(repo, cache)
	categoryhttp.RegisterRoutes(root, svc, authCheck)
}

// registerStorage 装配 storage 领域。
func registerStorage(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	storage := storageinfra.NewObjectStorage()
	svc := storageapp.NewStorageService(storage)
	storagehttp.RegisterRoutes(root, svc, authCheck)
}

// registerUser 装配 user 领域。
func registerUser(root routing.RouterGroup, svc userapp.UserService, authCheck routing.HandlerFunc) {
	userhttp.RegisterRoutes(root, svc, authCheck)
}

// registerAuth 装配 auth 领域。
func registerAuth(root routing.RouterGroup, deps *Deps, authCheck routing.HandlerFunc) {
	session := authinfra.NewSaTokenSession()
	userStore := NewUserSessionStore(deps.DB.Get())
	verify := authinfra.NewVerificationStore()
	email := authinfra.NewEmailSender()
	oauthReg := authinfra.NewOAuthProviderRegistry()
	svc := authapp.NewAuthService(session, userStore, verify, email, oauthReg)
	authhttp.RegisterRoutes(root, svc, authCheck)
}

// registerCircle 装配 circle 领域。
func registerCircle(root routing.RouterGroup, svc circleapp.CircleService, authCheck routing.HandlerFunc) {
	circlehttp.RegisterRoutes(root, svc, authCheck)
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck)
}

// ===== Service 构造函数 =====

func newUserService(deps *Deps) userapp.UserService {
	repo := userinfra.NewUserRepository(deps.DB.Get())
	cache := userinfra.NewUserCache()
	searcher := userinfra.NewUserSearcher()
	return userapp.NewUserService(repo, cache, searcher)
}

// newCircleService 构造 CircleService 并返回其依赖的 repo（供桥接器使用）。
func newCircleService(deps *Deps) (circleapp.CircleService, circledomain.CircleRepository, circledomain.MemberRepository) {
	circleRepo := circleinfra.NewCircleRepository(deps.DB.Get())
	memberRepo := circleinfra.NewMemberRepository(deps.DB.Get())
	baseCache := circleinfra.NewCircleBaseCache()
	statsCache := circleinfra.NewCircleStatsCache()
	joinedCache := circleinfra.NewJoinedCirclesCache()
	searcher := circleinfra.NewCircleSearcher()
	publisher := circleinfra.NewCircleEventPublisher()

	svc := circleapp.NewCircleService(
		circleRepo, memberRepo, baseCache, statsCache, joinedCache, searcher, publisher,
	)
	return svc, circleRepo, memberRepo
}

func newPostService(deps *Deps) postapp.PostService {
	repo := postinfra.NewPostRepository(deps.DB.Get())
	statsCache := postinfra.NewPostStatsCache()
	likeCache := postinfra.NewPostLikeCache()
	searcher := postinfra.NewPostSearcher()
	publisher := postinfra.NewPostEventPublisher()
	return postapp.NewPostService(repo, statsCache, likeCache, searcher, publisher)
}
