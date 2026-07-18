package http

import (
	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 circle 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//   POST /circle/create       创建圈子（需登录）
//   GET  /circle/list         搜索圈子列表（访客可读）
//   GET  /circle/active       近期活跃圈子列表（访客可读）
//   GET  /circle/detail/:id   获取圈子详情（访客可读；登录时回填 is_joined）
//   GET  /circle/my           我加入的圈子列表（需登录）
//   GET  /circle/user         任意用户加入圈子列表（访客可读）
//   POST /circle/join         加入圈子（需登录）
//   POST /circle/leave        退出圈子（需登录）
//   GET  /circle/posts        圈内帖子列表（访客可读）
//
// 访客可读端点（list/active/detail/user/posts）挂 optionalCheck；写操作与个人列表挂 authCheck。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CircleService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// 访客可读：搜索/活跃/详情/指定用户圈子/圈内帖。
	pub := rg.Group("/circle", optionalCheck)
	{
		pub.GET("/list", h.GetCircles)
		pub.GET("/active", h.GetActiveCircles)
		pub.GET("/detail/:id", h.GetCircleDetail)
		pub.GET("/user", h.GetUserCircles)
		pub.GET("/posts", h.GetCirclePosts)
	}

	// 需登录：建圈/我的圈子/加圈/退圈。
	priv := rg.Group("/circle", authCheck)
	{
		priv.POST("/create", h.CreateCircle)
		priv.GET("/my", h.GetMyCircles)
		priv.POST("/join", h.JoinCircle)
		priv.POST("/leave", h.LeaveCircle)
	}
}
