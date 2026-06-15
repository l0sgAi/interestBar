package http

import (
	"interestBar/pkg/domains/user/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 user 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /user 组一致）：
//   GET /user/get          获取当前会话用户（需登录）
//   PUT /user/update       修改资料（需登录）
//   GET /user/search       搜索用户（需登录）
//   GET /user/detail/:id   获取用户详情（需登录）
//
// 注意：旧 routers.go 中 /auth/logout 和 /auth/me 这两条路由虽然调用的
// 是 UserController.Logout / GetCurrentUser，但语义上属于"会话/鉴权"
// 而非"用户资料"，因此随 auth 领域一起迁移（见 pkg/domains/auth）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.UserService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	u := rg.Group("/user", authCheck)
	{
		u.GET("/get", h.GetCurrentUser)
		u.PUT("/update", h.UpdateProfile)
		u.GET("/search", h.SearchUsers)
		u.GET("/detail/:id", h.GetUserDetail)
	}
}
