package domain

import (
	"context"

	"github.com/google/uuid"
)

// CategoryRepository 是 category 领域的持久化接口。
//
// 由 infrastructure 层实现（PostgreSQL）。定义在 domain 层是为了让
// 业务代码依赖抽象而非具体存储——未来切换存储或拆分服务时，
// 只需替换实现，业务代码不变。
type CategoryRepository interface {
	// GetActiveCategories 获取启用的顶级分类列表。
	GetActiveCategories(ctx context.Context) ([]Category, error)
	// GetCategoriesByParentID 根据父分类 ID 获取子分类列表。
	GetCategoriesByParentID(ctx context.Context, parentID uuid.UUID) ([]Category, error)
	// GetAllActiveCategories 获取所有启用的分类列表。
	GetAllActiveCategories(ctx context.Context) ([]Category, error)
}

// CategoryCache 是 category 领域的缓存接口（可选，由 infrastructure 实现）。
//
// 拆出接口便于测试 mock，以及未来缓存实现替换（Redis / 本地 LRU）。
type CategoryCache interface {
	// GetCategories 从缓存读取分类列表。未命中时返回 nil, nil（不是 error）。
	GetCategories(ctx context.Context) ([]Category, error)
	// SetCategories 写入分类列表缓存。
	SetCategories(ctx context.Context, categories []Category) error
}
