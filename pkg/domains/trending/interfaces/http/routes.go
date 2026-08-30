package http

import (
	"interestBar/pkg/domains/trending/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 trending 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /trending?window=...&section=...&size=...&offset=...  热点看板（访客可读）
//	  window  = 24h | 7d（默认 24h）
//	  section = all | posts | circles | users（默认 all，首屏一次返回三类各 size 条）
//	  size    = 每板块条数（默认 20，上限 50）
//	  offset  = 单板块翻页偏移（section=all 时忽略）
//
// 访客可读：登录时 service 回填帖子的 is_liked/is_collected；匿名（uuid.Nil）时
// fillPosts 的 BatchCheck 是 best-effort，失败回 nil map，IsLiked/IsCollected 自然 false。
//
// 独立 /trending 路由组（不挂在 /post 下，因热点不限于帖子）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.TrendingService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)
	// 整组对访客开放（本域只有这一个只读端点）。authCheck 参数保留以统一注册签名。
	_ = authCheck
	t := rg.Group("/trending", optionalCheck)
	{
		t.GET("/", h.GetTrending)
	}
}
