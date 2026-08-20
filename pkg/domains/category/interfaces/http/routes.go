package http

import (
	"interestBar/pkg/domains/category/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 category 领域的路由挂到给定的路由组根上。
//
// 注意：调用方需保证传入的 rg 已经挂了全局中间件（CORS/log 等），
// 但**不需要**预先挂鉴权——category 路由的鉴权由本函数内部按需添加。
//
// 参数：
//   - authCheck      "需要登录"的中间件（对应 composition.RequireLogin），登录态强校验。
//   - optionalCheck  "可选登录"的中间件（对应 composition.OptionalLoginFn），有 token 则解析、
//     无/坏 token 静默放行；用于对访客开放的只读接口。
//
// 两者均由 composition 层注入，避免领域包 import sa-token 集成。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CategoryService,
	authCheck routing.HandlerFunc,
	optionalCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// /category/get 是全局只读分类列表，对访客开放（handler 不读 userID）。
	cat := rg.Group("/category", optionalCheck)
	{
		cat.GET("/get", h.GetCategories)
	}
}
