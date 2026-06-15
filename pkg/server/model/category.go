package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Category 圈子分类表
type Category struct {
	BaseModel
	Name        string     `json:"name" gorm:"column:name;type:varchar(50);not null"`     // 分类名称
	Slug        string     `json:"slug,omitempty" gorm:"column:slug;type:varchar(60)"`    // SEO友好标识
	Icon        string     `json:"icon,omitempty" gorm:"column:icon;type:varchar(500)"`   // 图标/Icon URL
	ParentID    *uuid.UUID `json:"parent_id,omitempty" gorm:"column:parent_id;type:uuid"` // 父分类ID，NULL表示顶级分类
	Sort        int        `json:"sort" gorm:"column:sort;default:0"`                     // 排序权重
	CircleCount int        `json:"circle_count" gorm:"column:circle_count;default:0"`     // 该分类下的圈子数量
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`   // 0=禁用/隐藏，1=启用/显示
	Deleted     int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"` // 逻辑删除
}

// TableName 指定表名
func (Category) TableName() string {
	return "domains.category"
}

// CategoryStatus 分类状态常量
const (
	CategoryStatusDisabled = 0 // 禁用/隐藏
	CategoryStatusEnabled  = 1 // 启用/显示
)

// GetActiveCategories 获取启用的顶级分类列表
func GetActiveCategories(db *gorm.DB) ([]Category, error) {
	var categories []Category
	err := db.Where("parent_id IS NULL AND status = ? AND deleted = ?", CategoryStatusEnabled, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}

// GetCategoriesByParentID 根据父分类ID获取子分类列表
func GetCategoriesByParentID(db *gorm.DB, parentID uuid.UUID) ([]Category, error) {
	var categories []Category
	err := db.Where("parent_id = ? AND deleted = ?", parentID, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}

// GetAllActiveCategories 获取所有启用的分类列表
func GetAllActiveCategories(db *gorm.DB) ([]Category, error) {
	var categories []Category
	err := db.Where("status = ? AND deleted = ?", CategoryStatusEnabled, 0).
		Order("sort DESC").
		Find(&categories).Error
	return categories, err
}
