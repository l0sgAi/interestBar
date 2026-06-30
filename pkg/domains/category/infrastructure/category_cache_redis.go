package infrastructure

import (
	"context"
	"time"

	"interestBar/pkg/domains/category/domain"
	redispkg "interestBar/pkg/server/storage/redis"
)

// categoriesCacheKey 分类列表在 Redis 中的 key（与旧 controller/category.go 保持一致）。
const categoriesCacheKey = "categories:all"

// categoriesCacheTTL 缓存有效期（与旧实现一致：1 小时）。
const categoriesCacheTTL = 1 * time.Hour

// categoryCacheRedis 基于 Redis 的 CategoryCache 实现。
//
// 它复用项目既有 pkg/server/storage/redis 包的 GetJSON/SetJSON 工具，
// 不直接持有 *redis.Client，保持与连接管理解耦。
type categoryCacheRedis struct{}

// NewCategoryCache 构造一个基于 Redis 的 CategoryCache。
func NewCategoryCache() domain.CategoryCache {
	return &categoryCacheRedis{}
}

// GetCategories 从 Redis 读取分类列表。
// 未命中时返回 nil, nil（application 层据此回源数据库）。
func (c *categoryCacheRedis) GetCategories(ctx context.Context) ([]domain.Category, error) {
	var categories []domain.Category
	err := redispkg.GetJSON(categoriesCacheKey, &categories)
	if err != nil {
		// 缓存未命中或反序列化失败，均按"未命中"处理，回源 DB。
		// 不把 error 透出，避免 application 层需要区分"缓存故障"和"缓存未命中"。
		return nil, nil
	}
	return categories, nil
}

// SetCategories 将分类列表写入 Redis。
func (c *categoryCacheRedis) SetCategories(ctx context.Context, categories []domain.Category) error {
	return redispkg.SetJSON(categoriesCacheKey, categories, categoriesCacheTTL)
}
