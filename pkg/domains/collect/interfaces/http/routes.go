package http

import (
	"interestBar/pkg/domains/collect/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 collect 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	POST /collect/toggle   收藏/取消收藏（需登录）
//	GET  /collect/posts    我的收藏列表（需登录，keyset 分页）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CollectService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	g := rg.Group("/collect", authCheck)
	{
		g.POST("/toggle", h.ToggleCollect)
		g.GET("/posts", h.ListCollectedPosts)
	}
}
