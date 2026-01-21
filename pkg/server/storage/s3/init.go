package s3

import (
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"sync"

	"go.uber.org/zap"
)

var (
	// S3Client 全局 S3 客户端实例
	S3Client *Client
	// once 确保只初始化一次
	once sync.Once
)

// InitS3Client 初始化 S3 客户端
func InitS3Client() error {
	var initErr error
	once.Do(func() {
		// 从配置文件获取 S3 配置
		s3Config := conf.Config.S3

		// 检查必要配置
		if s3Config.AccessKeyID == "" || s3Config.SecretAccessKey == "" || s3Config.Region == "" || s3Config.Bucket == "" {
			initErr = fmt.Errorf("S3 configuration is incomplete: access_key_id, secret_access_key, region, and bucket are required")
			return
		}

		// 创建 S3 客户端
		client, err := NewClient(
			s3Config.AccessKeyID,
			s3Config.SecretAccessKey,
			s3Config.Region,
			s3Config.Bucket,
			s3Config.Endpoint,
			s3Config.PresignURLExpire,
			s3Config.CloudfrontDomain,
			logger.Log,
		)
		if err != nil {
			initErr = fmt.Errorf("failed to create S3 client: %w", err)
			return
		}

		S3Client = client
		logger.Log.Info("S3 client initialized successfully",
			zap.String("bucket", s3Config.Bucket),
			zap.String("region", s3Config.Region),
		)
	})

	return initErr
}

// GetS3Client 获取 S3 客户端实例
func GetS3Client() *Client {
	if S3Client == nil {
		logger.Log.Error("S3 client is not initialized, call InitS3Client first")
		return nil
	}
	return S3Client
}
