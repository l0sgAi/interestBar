package router

import (
	"interestBar/pkg/conf"
	"interestBar/pkg/server/controller"

	"github.com/click33/sa-token-go/stputil"
	"github.com/gin-gonic/gin"
)

// SaTokenAuth Sa-Token 认证中间件
func SaTokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从配置文件获取请求头名称
		tokenName := conf.Config.SaToken.TokenName

		// 从 Header 获取 token
		token := c.GetHeader(tokenName)
		if token == "" {
			c.JSON(401, gin.H{"code": 401, "message": "Token not found"})
			c.Abort()
			return
		}

		// 使用 Sa-Token-Go 验证登录状态
		err := stputil.CheckLogin(token)
		if err != nil {
			c.JSON(401, gin.H{"code": 401, "message": "Invalid or expired token"})
			c.Abort()
			return
		}

		// 获取登录 ID 并存入上下文
		loginID, err := stputil.GetLoginID(token)
		if err != nil {
			c.JSON(401, gin.H{"code": 401, "message": "Failed to get login ID"})
			c.Abort()
			return
		}

		c.Set("login_id", loginID)
		c.Set("token", token)

		c.Next()
	}
}

func RegisterRoutes(r *gin.RouterGroup) {
	// System / Hello
	sysCtrl := controller.NewSystemController()
	r.GET("/health", sysCtrl.HealthCheck)

	helloCtrl := controller.NewHelloController()
	r.GET("/hello", helloCtrl.SayHello)

	// Auth routes (no authentication required)
	auth := r.Group("auth")
	{
		userCtrl := controller.NewUserController()
		auth.GET("google/login", userCtrl.GoogleLogin)
		auth.GET("google/callback", userCtrl.GoogleCallback)
		auth.POST("logout", SaTokenAuth(), userCtrl.Logout)
		auth.GET("me", SaTokenAuth(), userCtrl.GetCurrentUser)
	}

	// User routes
	userCtrl := controller.NewUserController()
	user := r.Group("user")
	{
		user.GET("get", SaTokenAuth(), userCtrl.GetUser)
	}

	// Example: Protected routes with authentication
	protected := r.Group("api")
	protected.Use(SaTokenAuth())
	{
		// Add your protected endpoints here
		protected.GET("/profile", func(c *gin.Context) {
			loginID, _ := c.Get("login_id")
			token, _ := c.Get("token")
			c.JSON(200, gin.H{
				"message": "This is a protected route",
				"user_id": loginID,
				"token":   token,
			})
		})
	}

	// Example: Admin routes with role-based access control
	admin := r.Group("admin")
	admin.Use(SaTokenAuth())
	{
		admin.GET("/dashboard", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"message": "Welcome to admin dashboard",
			})
		})
	}
}
