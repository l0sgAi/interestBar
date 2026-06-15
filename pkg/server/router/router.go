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

	// Register Routes（旧路径：尚未搬迁的领域）
	root := r.Group("")
	RegisterRoutes(root)

	// Register Domain Routes（新路径：已搬迁到 pkg/domains/ 的领域）
	// 目前仅 category；其余领域仍在 RegisterRoutes 中。
	composition.RegisterDomainRoutes(r)

	if logger.Log != nil {
		logger.Log.Info("router register success")
	}
	return r
}
