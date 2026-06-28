package http

import (
	"interestBar/pkg/domains/recommend/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 recommend 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /post/home?tab=recommend  首页推荐流（需登录）
//
// 复用 /post 前缀（home 是 post 列表的 tab 化入口）；其它 tab（following/hot/latest）TODO。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.RecommendService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)
	p := rg.Group("/post", authCheck)
	{
		p.GET("/home", h.GetHomeFeed)
	}
}
