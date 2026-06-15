package router

import (
	"interestBar/pkg/composition"
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

	// Register Domain Routes（所有领域已搬迁到 pkg/domains/）
	composition.RegisterDomainRoutes(r)

	if logger.Log != nil {
		logger.Log.Info("router register success")
	}
	return r
}
