package router

import (
	"interestBar/pkg/server/controller"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.RouterGroup) {
	// System / Hello
	sysCtrl := controller.NewSystemController()
	r.GET("/health", sysCtrl.HealthCheck)

	helloCtrl := controller.NewHelloController()
	r.GET("/hello", helloCtrl.SayHello)

	// User
	userCtrl := controller.NewUserController()
	user := r.Group("user")
	{
		user.POST("register", userCtrl.Register)
		user.POST("login", userCtrl.Login)
		user.GET("get", userCtrl.GetUser)
	}

	// Auth
	auth := r.Group("auth")
	{
		auth.GET("google/login", userCtrl.GoogleLogin)
		auth.GET("google/callback", userCtrl.GoogleCallback)
	}
}
