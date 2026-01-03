package router

import (
	"interestBar/pkg/server/controller"
	"interestBar/pkg/server/router/middleware"

	"github.com/gin-gonic/gin"
)

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
		auth.POST("logout", middleware.Auth(), userCtrl.Logout)
		auth.GET("me", middleware.Auth(), userCtrl.GetCurrentUser)
	}

	// User routes
	userCtrl := controller.NewUserController()
	user := r.Group("user")
	{
		user.GET("get", middleware.Auth(), userCtrl.GetUser)
	}

	// Example: Protected routes with authentication
	protected := r.Group("api")
	protected.Use(middleware.Auth())
	{
		// Add your protected endpoints here
		protected.GET("/profile", func(c *gin.Context) {
			userID, _ := c.Get("user_id")
			email, _ := c.Get("email")
			c.JSON(200, gin.H{
				"message": "This is a protected route",
				"user_id": userID,
				"email":   email,
			})
		})
	}

	// Example: Admin routes with role-based access control
	admin := r.Group("admin")
	admin.Use(middleware.Auth(), middleware.RoleAuth(1)) // Require role >= 1 (admin)
	{
		admin.GET("/dashboard", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"message": "Welcome to admin dashboard",
			})
		})
	}
}
