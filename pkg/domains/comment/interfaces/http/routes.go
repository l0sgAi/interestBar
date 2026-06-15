package http

import (
	"interestBar/pkg/domains/comment/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 comment 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /comment 组一致）：
//   POST /comment/create       发评论/回复（需登录）
//   GET  /comment/list         获取顶层评论列表（需登录）
//   GET  /comment/replies      获取楼层内回复列表（需登录）
//   GET  /comment/detail/:id   获取单条评论详情（需登录）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CommentService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	cm := rg.Group("/comment", authCheck)
	{
		cm.POST("/create", h.CreateComment)
		cm.GET("/list", h.GetComments)
		cm.GET("/replies", h.GetReplies)
		cm.GET("/detail/:id", h.GetCommentDetail)
	}
}
