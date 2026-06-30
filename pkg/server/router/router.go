package router

import (
	"fmt"

	"interestBar/pkg/composition"
	"interestBar/pkg/composition/hertzadapter"
	"interestBar/pkg/composition/middleware"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	"github.com/cloudwego/hertz/pkg/app/server"
)

// InitRouter 创建并装配 hertz server，返回 *server.Hertz。
//
// 不在这里调用 Spin/Run（阻塞），由调用方决定运行方式，便于资源清理编排。
// server.Default() 已自带 Recovery；这里额外挂 Logger 和 CORS。
func InitRouter() *server.Hertz {
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf(":%d", conf.Config.Server.Port)),
		// hertz 默认 MaxRequestBodySize=4MB，会截断图片上传请求体导致 FormFile 报错返 400。
		// 提到 50MB；具体文件大小仍由 service 层 ValidateFile 兜底校验。
		server.WithMaxRequestBodySize(50<<20),
	)

	// Middleware
	h.Use(middleware.Logger())
	h.Use(middleware.CORS())

	// Register Domain Routes（所有领域已搬迁到 pkg/domains/）
	// 入口层做 engine→RouterGroup 的框架无关包装。
	composition.RegisterDomainRoutes(hertzadapter.ForEngine(h))

	if logger.Log != nil {
		logger.Log.Info("router register success")
	}
	return h
}
