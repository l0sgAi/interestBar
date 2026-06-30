// Package infrastructure 提供 category 领域基础设施层实现：
// PostgreSQL 持久化 + Redis 缓存。
//
// 这些实现 domain 层定义的 Repository / CategoryCache 接口。
// 业务代码（application 层）只依赖 domain 接口，不感知这里的实现细节。
package infrastructure

import (
	"context"

	"interestBar/pkg/domains/category/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// categoryRepoPG 基于 GORM 的 CategoryRepository 实现。
type categoryRepoPG struct {
	db *gorm.DB
}

// NewCategoryRepository 构造一个基于 PostgreSQL 的 CategoryRepository。
func NewCategoryRepository(db *gorm.DB) domain.CategoryRepository {
	return &categoryRepoPG{db: db}
}

// GetActiveCategories 获取启用的顶级分类列表。
func (r *categoryRepoPG) GetActiveCategories(ctx context.Context) ([]domain.Category, error) {
	var categories []domain.Category
	err := r.db.WithContext(ctx).
		Where("parent_id IS NULL AND status = ? AND deleted = ?", domain.CategoryStatusEnabled, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}

// GetCategoriesByParentID 根据父分类 ID 获取子分类列表。
func (r *categoryRepoPG) GetCategoriesByParentID(ctx context.Context, parentID uuid.UUID) ([]domain.Category, error) {
	var categories []domain.Category
	err := r.db.WithContext(ctx).
		Where("parent_id = ? AND deleted = ?", parentID, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}

// GetAllActiveCategories 获取所有启用的分类列表。
func (r *categoryRepoPG) GetAllActiveCategories(ctx context.Context) ([]domain.Category, error) {
	var categories []domain.Category
	err := r.db.WithContext(ctx).
		Where("status = ? AND deleted = ?", domain.CategoryStatusEnabled, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}
