// Package composition 的 server.go：装配各领域依赖并注册路由。
package composition

import (
	"time"

	authapp "interestBar/pkg/domains/auth/application"
	authinfra "interestBar/pkg/domains/auth/infrastructure"
	authhttp "interestBar/pkg/domains/auth/interfaces/http"
	categoryapp "interestBar/pkg/domains/category/application"
	categoryinfra "interestBar/pkg/domains/category/infrastructure"
	categoryhttp "interestBar/pkg/domains/category/interfaces/http"
	circleapp "interestBar/pkg/domains/circle/application"
	circledomain "interestBar/pkg/domains/circle/domain"
	circleinfra "interestBar/pkg/domains/circle/infrastructure"
	circlehttp "interestBar/pkg/domains/circle/interfaces/http"
	commentapp "interestBar/pkg/domains/comment/application"
	commentinfra "interestBar/pkg/domains/comment/infrastructure"
	commenthttp "interestBar/pkg/domains/comment/interfaces/http"
	likeapp "interestBar/pkg/domains/like/application"
	likeinfra "interestBar/pkg/domains/like/infrastructure"
	likehttp "interestBar/pkg/domains/like/interfaces/http"
	postapp "interestBar/pkg/domains/post/application"
	postinfra "interestBar/pkg/domains/post/infrastructure"
	posthttp "interestBar/pkg/domains/post/interfaces/http"
	storageapp "interestBar/pkg/domains/storage/application"
	storageinfra "interestBar/pkg/domains/storage/infrastructure"
	storagehttp "interestBar/pkg/domains/storage/interfaces/http"
	userapp "interestBar/pkg/domains/user/application"
	userinfra "interestBar/pkg/domains/user/infrastructure"
	userhttp "interestBar/pkg/domains/user/interfaces/http"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/routing"
)

// RegisterDomainRoutes 把所有"已搬迁到 domains/"的领域路由挂到 Web server 上。
//
// root 是框架无关的 RouterGroup（由入口层用 composition/hertzadapter.ForEngine
// 从 *server.Hertz 包装而来）。这样本函数彻底不感知底层框架。
func RegisterDomainRoutes(root routing.RouterGroup) {
	deps := NewDeps()
	authCheck := RequireLogin

	// 先构造各领域 Service（不立即注册路由，因为要互注 Facade）
	userSvc := newUserService(deps)
	userFacade := userapp.NewUserFacade(userSvc)

	circleSvc, circleRepo, memberRepo := newCircleService(deps)
	circleFacade := circleapp.NewCircleFacade(circleRepo)

	postSvc := newPostService(deps)

	commentSvc := newCommentService(deps)
	likeSvc := newLikeService(deps)

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

	// comment 需要 user Facade + post 查询端口（帖子校验 + 评论计数）
	commentSvc.SetUserFacade(&commentUserFacade{delegate: userFacade})
	commentSvc.SetPostLookup(&commentPostLookup{delegate: postSvc})

	// like 需要 post 查询端口 + comment 查询端口（目标存在性校验 + 统计缓存恢复）
	likeSvc.SetPostTarget(&likePostTarget{delegate: postSvc})
	likeSvc.SetCommentTarget(&likeCommentTarget{delegate: commentSvc})

	// 跨领域 Facade 注入完成。如遗漏注入，相关领域会在请求时表现为空数据/校验失败，
	// 这里打一条启动日志便于排查（强类型断言成本过高，用日志替代 panic，见 review P2-2）。
	if logger.Log != nil {
		logger.Log.Info("cross-domain facades injected: circle<-user/post, post<-user/circle, comment<-user/post, like<-post/comment")
	}

	// 注册路由
	registerCategory(root, deps, authCheck)
	registerStorage(root, deps, authCheck)
	registerUser(root, userSvc, authCheck)
	registerAuth(root, deps, authCheck)
	registerCircle(root, circleSvc, authCheck)
	registerPost(root, postSvc, authCheck)
	registerComment(root, commentSvc, authCheck)
	registerLike(root, likeSvc, authCheck)
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

	// IP 滑动窗口限流：注册三端点共享一桶，登录单独一桶。
	registerLimiter := NewIPRateLimiter(IPRateLimitOpt{
		KeyPrefix: "rl:ip:auth-register", Limit: 10, Window: time.Minute,
	})
	loginLimiter := NewIPRateLimiter(IPRateLimitOpt{
		KeyPrefix: "rl:ip:auth-login", Limit: 10, Window: time.Minute,
	})
	authhttp.RegisterRoutes(root, svc, authCheck, registerLimiter, loginLimiter)
}

// registerCircle 装配 circle 领域。
func registerCircle(root routing.RouterGroup, svc circleapp.CircleService, authCheck routing.HandlerFunc) {
	circlehttp.RegisterRoutes(root, svc, authCheck)
}

// registerPost 装配 post 领域。
func registerPost(root routing.RouterGroup, svc postapp.PostService, authCheck routing.HandlerFunc) {
	posthttp.RegisterRoutes(root, svc, authCheck)
}

// registerComment 装配 comment 领域。
func registerComment(root routing.RouterGroup, svc commentapp.CommentService, authCheck routing.HandlerFunc) {
	commenthttp.RegisterRoutes(root, svc, authCheck)
}

// registerLike 装配 like 领域。
func registerLike(root routing.RouterGroup, svc likeapp.LikeService, authCheck routing.HandlerFunc) {
	likehttp.RegisterRoutes(root, svc, authCheck)
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

// newCommentService 构造 CommentService。
//
// user/post 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newCommentService(deps *Deps) commentapp.CommentService {
	repo := commentinfra.NewCommentRepository(deps.DB.Get())
	statsCache := commentinfra.NewCommentStatsCache()
	likeCache := commentinfra.NewCommentLikeCache()
	return commentapp.NewCommentService(repo, statsCache, likeCache)
}

// newLikeService 构造 LikeService。
//
// post/comment 跨领域依赖通过 setter 注入（见 RegisterDomainRoutes）。
func newLikeService(deps *Deps) likeapp.LikeService {
	postCache := likeinfra.NewPostLikeCache()
	commentCache := likeinfra.NewCommentLikeCache()
	publisher := likeinfra.NewLikeEventPublisher()
	return likeapp.NewLikeService(postCache, commentCache, publisher)
}
