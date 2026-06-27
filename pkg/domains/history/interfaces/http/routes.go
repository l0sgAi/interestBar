package http

import (
	"interestBar/pkg/domains/history/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 history 领域的路由挂到给定的路由组根上。
//
// 路由清单:
//
//	GET /history/posts  最近浏览列表(需登录,ZSET offset 分页)
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.HistoryService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	g := rg.Group("/history", authCheck)
	{
		g.GET("/posts", h.ListHistoryPosts)
	}
}
