package router

import (
	"interestBar/pkg/logger"
	"interestBar/pkg/server/controller"

	sagin "github.com/click33/sa-token-go/integrations/gin"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.RouterGroup) {
	// Auth routes (公开访问，不需要鉴权)
	auth := r.Group("auth")
	{
		userCtrl := controller.NewUserController()
		auth.GET("google/login", userCtrl.GoogleLogin)
		auth.GET("google/callback", userCtrl.GoogleCallback)
		auth.GET("github/login", userCtrl.GithubLogin)
		auth.GET("github/callback", userCtrl.GithubCallback)
		// logout 和 me 需要登录
		auth.POST("logout", sagin.CheckLogin(), userCtrl.Logout)
		auth.GET("me", sagin.CheckLogin(), userCtrl.GetCurrentUser)
	}

	// User routes (需要登录)
	userCtrl := controller.NewUserController()
	user := r.Group("user")
	{
		user.GET("get", sagin.CheckLogin(), userCtrl.GetUser)
		user.PUT("update", sagin.CheckLogin(), userCtrl.UpdateProfile)
	}

	// Upload routes (需要登录鉴权)
	uploadCtrl := controller.NewUploadController(logger.Log)
	upload := r.Group("upload")
	{
		// 上传图片接口 - 使用 sagin.CheckLogin() 进行鉴权
		upload.POST("/image", sagin.CheckLogin(), uploadCtrl.UploadImage)
	}

}
