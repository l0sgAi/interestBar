package apps

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/auth"
	"interestBar/pkg/server/router"
	s3storage "interestBar/pkg/server/storage/s3"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/elasticsearch"
	redpanda "interestBar/pkg/server/storage/redpanda"
	"interestBar/pkg/server/storage/redis"
	"os"
	"os/signal"
	"strings"
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

	// 7. Init Elasticsearch for full-text search
	if err := elasticsearch.InitElasticsearch(); err != nil {
		logger.Log.Warn("Failed to initialize Elasticsearch: " + err.Error())
		logger.Log.Info("Running without Elasticsearch search functionality")
	}

	// 8. Init Redpanda for async statistics persistence
	if err := redpanda.InitRedpandaProducer(); err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to initialize Redpanda producer: %s", err.Error()))
		logger.Log.Warn("Circle statistics persistence to database is disabled. Redis cache will still be updated in real-time.")
		logger.Log.Info("To enable Redpanda:")
		logger.Log.Info("  1. Check Redpanda is running at: " + strings.Join(conf.Config.Redpanda.Brokers, ","))
		logger.Log.Info("  2. Verify network connectivity to Redpanda brokers")
		logger.Log.Info("  3. Ensure topic 'circle_statistics' exists or set AllowAutoTopicCreation=true")
	} else {
		logger.Log.Info("Redpanda producer initialized successfully")
		// 启动消费者处理统计信息聚合（PostgreSQL持久化）
		go redpanda.StartStatisticsConsumerWithRetry()
	}

	// 9. Init Router
	r := router.InitRouter()

	// 10. Run Server
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
	redpanda.CloseRedpandaProducer()
	logger.Log.Info("Server shutdown complete")
}
