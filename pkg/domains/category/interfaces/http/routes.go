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
// 参数 authCheck 是"需要登录"的中间件（对应 composition.RequireLogin），
// 由 composition 层注入，避免领域包 import sa-token 集成。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.CategoryService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	// /category 组：所有接口都需要登录
	cat := rg.Group("/category", authCheck)
	{
		cat.GET("/get", h.GetCategories)
	}
}
