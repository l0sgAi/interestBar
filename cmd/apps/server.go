package apps

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/router"
	s3storage "interestBar/pkg/server/storage/s3"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/redis"
	"os"
	"os/signal"
	"syscall"
)

func Run(configPath string) {
	// 1. Init Config
	conf.InitConfig(configPath)

	// 2. Init Logger
	logger.InitLogger()

	// 3. Init DB
	pgsql.InitDB()

	// 4. Init Redis for caching
	redisAddr := fmt.Sprintf("%s:%d", conf.Config.Redis.Host, conf.Config.Redis.Port)
	if err := redis.InitRedis(redisAddr, conf.Config.Redis.Password, conf.Config.Redis.D); err != nil {
		logger.Log.Fatal("Failed to initialize Redis: " + err.Error())
	}
	logger.Log.Info("Redis initialized successfully for caching")

	// 5. Init Sa-Token (includes Redis connection)
	if err := auth.InitSaToken(); err != nil {
		logger.Log.Fatal("Failed to initialize Sa-Token: " + err.Error())
	}

	// 6. Init S3 Client for file storage
	if err := s3storage.InitS3Client(); err != nil {
		logger.Log.Fatal("Failed to initialize S3 client: " + err.Error())
	}

	// 7. Init Router
	r := router.InitRouter()

	// 8. Run Server
	addr := fmt.Sprintf(":%d", conf.Config.Server.Port)
	logger.Log.Info("Server starting on " + addr)

	go func() {
		if err := r.Run(addr); err != nil {
			logger.Log.Fatal("Server start failed: " + err.Error())
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Log.Info("Shutdown Server ...")

	// Close resources
	redis.CloseRedis()
	auth.CloseSaToken()
	logger.Log.Info("Server shutdown complete")
}
