package http

import (
	"interestBar/pkg/domains/recommend/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 recommend 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /post/home?tab=...  首页信息流（需登录）
//	  tab=recommend  推荐流（候选池 offset + pool_token；5 路召回 + CF）
//	  tab=hot         全局热门（rank_score 时间衰减，search_after 翻页）
//	  tab=latest      全局最新（create_time desc，search_after 翻页）
//	  tab=following   关注流（已加入圈子，时间倒序，search_after 翻页）
//
// 复用 /post 前缀（home 是 post 列表的 tab 化入口）。
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
