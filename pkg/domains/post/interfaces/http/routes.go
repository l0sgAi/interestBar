package http

import (
	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 post 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /post 组一致）：
//   POST /post/create       发帖（需登录）
//   GET  /post/list         搜索帖子列表（需登录）
//   GET  /post/my           查看自己发的帖（需登录）
//   GET  /post/user/:user_id 查看任意用户发的帖（需登录，仅已发布）
// RegisterRoutes registers the post domain HTTP routes onto the provided router group with authentication required for all endpoints.
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.PostService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	p := rg.Group("/post", authCheck)
	{
		p.POST("/create", h.CreatePost)
		p.GET("/list", h.GetPosts)
		p.GET("/my", h.GetMyPosts)
		p.GET("/user/:user_id", h.GetUserPosts)
		p.GET("/detail/:id", h.GetPostDetail)
	}
}
