package infrastructure

import (
	"context"
	"time"

	"interestBar/pkg/domains/user/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// userInfoCacheTTL 用户信息缓存有效期（与旧 controller/user.go 一致：30 分钟）。
const userInfoCacheTTL = 30 * time.Minute

// userCacheRedis 基于 Redis 的 UserCache 实现。
//
// 复用 pkg/server/storage/redis 包的 GetJSONCompressed/SetJSONCompressed，
// 保持与旧 controller GetUserDetail 的压缩缓存行为一致。
type userCacheRedis struct{}

// NewUserCache 构造一个基于 Redis 的 UserCache。
func NewUserCache() domain.UserCache {
	return &userCacheRedis{}
}

// GetUser 从 Redis 读取用户信息。未命中返回 nil, nil。
func (c *userCacheRedis) GetUser(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error) {
	key := redispkg.GetUserInfoKey(userID)
	var user domain.SysUser
	if err := redispkg.GetJSONCompressed(key, &user); err != nil {
		// 未命中或反序列化失败，均按"未命中"处理。
		return nil, nil
	}
	return &user, nil
}

// SetUser 写入用户信息缓存（压缩）。
func (c *userCacheRedis) SetUser(ctx context.Context, userID uuid.UUID, user *domain.SysUser) error {
	key := redispkg.GetUserInfoKey(userID)
	return redispkg.SetJSONCompressed(key, user, userInfoCacheTTL)
}
