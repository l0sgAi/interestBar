package apps

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/router"
	"interestBar/pkg/server/storage/db/pgsql"
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

	// 4. Init Router
	r := router.InitRouter()

	// 5. Run Server
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
}
