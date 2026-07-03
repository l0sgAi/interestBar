package http

import (
	"interestBar/pkg/domains/discover/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 discover 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET /discover?section=...&size=...&offset=...&pool_token=...  发现页（允许匿名）
//	  section   = all | posts | circles（默认 all，首屏一次返回两类各 size 条）
//	  size      = 每分区条数（默认 20，上限 50）
//	  offset    = 单分区翻页偏移（section=all 时忽略）
//	  pool_token = 候选池版本；不匹配→重建 + 回 offset=0
//
// 独立 /discover 路由组（不挂在 /post 下，因发现含圈子）。
// 注意：authCheck 这里实际传入 OptionalLogin（允许匿名），而非 RequireLogin。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.DiscoverService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)
	d := rg.Group("/discover", authCheck)
	{
		d.GET("/", h.GetDiscover)
	}
}
