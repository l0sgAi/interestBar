package router

import (
	"interestBar/pkg/logger"
	"interestBar/pkg/server/router/middleware"

	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {
	r := gin.New()

	// Middleware
	r.Use(middleware.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS()) // 添加 CORS 中间件

	// Register Routes
	root := r.Group("")
	RegisterRoutes(root)

	if logger.Log != nil {
		logger.Log.Info("router register success")
	}
	return r
}
