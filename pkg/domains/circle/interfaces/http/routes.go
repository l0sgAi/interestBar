package http

import (
	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 circle 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /circle 组一致）：
//   POST /circle/create       创建圈子（需登录）
//   GET  /circle/list         搜索圈子列表（需登录）
//   GET  /circle/active       近期活跃圈子列表（需登录）
//   GET  /circle/detail/:id   获取圈子详情（需登录）
//   GET  /circle/my           我加入的圈子列表（需登录）
//   GET  /circle/user         任意用户加入圈子列表（需登录）
//   POST /circle/join         加入圈子（需登录）
//   POST /circle/leave        退出圈子（需登录）
//   GET  /circle/posts        圈内帖子列表（需登录）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CircleService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	cir := rg.Group("/circle", authCheck)
	{
		cir.POST("/create", h.CreateCircle)
		cir.GET("/list", h.GetCircles)
		cir.GET("/active", h.GetActiveCircles)
		cir.GET("/detail/:id", h.GetCircleDetail)
		cir.GET("/my", h.GetMyCircles)
		cir.GET("/user", h.GetUserCircles)
		cir.POST("/join", h.JoinCircle)
		cir.POST("/leave", h.LeaveCircle)
		cir.GET("/posts", h.GetCirclePosts)
	}
}
