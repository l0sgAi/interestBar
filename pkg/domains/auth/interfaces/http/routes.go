package http

import (
	"interestBar/pkg/domains/auth/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 auth 领域的路由挂到给定的路由组根上。
//
// 路由清单（与旧 routers.go 中 /auth 组一致）：
//
//	公开（无需登录）：
//	  GET  /auth/google/login       OAuth 登录跳转
//	  GET  /auth/google/callback    OAuth 回调
//	  GET  /auth/github/login
//	  GET  /auth/github/callback
//	  GET  /auth/azure/login
//	  GET  /auth/azure/callback
//	  POST /auth/register/send-code 发送验证码（IP 限流）
//	  POST /auth/register/verify    校验验证码（IP 限流 + 失败次数硬上限）
//	  POST /auth/register/complete  完成注册（IP 限流）
//	  POST /auth/login              邮箱密码登录（IP 限流）
//
//	需要登录：
//	  POST /auth/logout             注销当前 token
//
// 注意：旧 routers.go 把 /auth/me (GetCurrentUser) 也放在这里，
// 但它实际返回的是 user 领域的数据。为了职责清晰，/auth/me 由 user 领域
// 注册到 /user/get（行为等价）。如果前端依赖 /auth/me，可在 user 领域
// 再加一条同名路由——但当前选择"语义归位"：会话查询统一走 /user/*。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.AuthService,
	authCheck routing.HandlerFunc,
	registerLimiter routing.HandlerFunc,
	loginLimiter routing.HandlerFunc,
) {
	h := NewHandler(svc)

	auth := rg.Group("/auth")
	{
		// 公开路由 —— OAuth（每个 provider 显式注册，与旧 routers.go 一致）
		for _, p := range []string{"google", "github", "azure"} {
			auth.GET("/"+p+"/login", h.OAuthLogin(p))
			auth.GET("/"+p+"/callback", h.OAuthCallback(p))
		}

		// 注册三端点共享同一 IP 限流桶（防组合爆破 / 邮件轰炸）。
		registerGrp := auth.Group("/register", registerLimiter)
		registerGrp.POST("/send-code", h.SendCode)
		registerGrp.POST("/verify", h.VerifyCode)
		registerGrp.POST("/complete", h.CompleteRegistration)

		// 登录单挂限流（防密码爆破）。
		auth.POST("/login", loginLimiter, h.Login)

		// 需要登录：注销。用子组挂中间件。
		logoutGrp := auth.Group("/", authCheck)
		logoutGrp.POST("/logout", h.Logout)
	}
}
