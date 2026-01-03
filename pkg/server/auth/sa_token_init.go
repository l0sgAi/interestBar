package auth

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"

	sagin "github.com/click33/sa-token-go/integrations/gin"
	"github.com/click33/sa-token-go/storage/redis"
)

// InitSaToken 初始化 Sa-Token-Go 框架
func InitSaToken() error {
	// 创建 Redis 存储 (使用完整的 Redis URL)
	var redisURL string

	// 格式: redis://[password@]host:port/db
	if conf.Config.Redis.Password != "" {
		// 有密码的情况: redis://password@host:port/db
		redisURL = fmt.Sprintf("redis://%s@%s:%d/%d",
			conf.Config.Redis.Password,
			conf.Config.Redis.Host,
			conf.Config.Redis.Port,
			conf.Config.Redis.D,
		)
	} else {
		// 无密码的情况: redis://host:port/db
		redisURL = fmt.Sprintf("redis://%s:%d/%d",
			conf.Config.Redis.Host,
			conf.Config.Redis.Port,
			conf.Config.Redis.D,
		)
	}

	storage, err := redis.NewStorage(redisURL)
	if err != nil {
		return fmt.Errorf("failed to create redis storage: %w", err)
	}

	// 使用配置文件中的 Sa-Token 配置
	config := sagin.DefaultConfig()

	// 如果配置文件中有 Sa-Token 配置,则使用配置文件的值
	if conf.Config.SaToken.TokenName != "" {
		config.TokenName = conf.Config.SaToken.TokenName
	}
	if conf.Config.SaToken.Timeout > 0 {
		config.Timeout = int64(conf.Config.SaToken.Timeout)
	} else {
		config.Timeout = 259200 // 默认3天
	}
	if conf.Config.SaToken.ActiveTimeout > 0 {
		config.ActiveTimeout = int64(conf.Config.SaToken.ActiveTimeout)
	} else {
		config.ActiveTimeout = 1800 // 默认30分钟
	}
	config.IsConcurrent = conf.Config.SaToken.IsConcurrent
	config.IsShare = conf.Config.SaToken.IsShare
	config.IsLog = true

	// 创建 Sa-Token 管理器
	manager := sagin.NewManager(storage, config)

	// 设置全局管理器
	sagin.SetManager(manager)

	logger.Log.Info("Sa-Token initialized successfully")
	logger.Log.Info(fmt.Sprintf("Token timeout: %d seconds", config.Timeout))
	logger.Log.Info(fmt.Sprintf("Token name: %s", config.TokenName))

	return nil
}

// CloseSaToken 关闭 Sa-Token 连接
func CloseSaToken() error {
	if sagin.GetManager() != nil && sagin.GetManager().GetStorage() != nil {
		return sagin.GetManager().GetStorage().(interface {
			Close() error
		}).Close()
	}
	return nil
}
