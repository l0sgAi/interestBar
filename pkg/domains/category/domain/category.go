// Package domain 存放 category 领域的纯领域模型：实体、值对象、常量、
// 以及领域定义的 Repository 接口（由 infrastructure 层实现）。
//
// 依赖规则：本包不得 import 任何 gorm/redis/elasticsearch 等基础设施库，
// 也不得 import 其他领域包（domains/circle 等）。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Category 圈子分类聚合根。
//
// 与旧 model.Category 字段一一对应（含 TableName），仅迁移了归属：
// 现在它属于 category 领域，未来拆分微服务时整体随领域包迁移。
type Category struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	Name        string     `json:"name" gorm:"column:name;type:varchar(50);not null"`
	Slug        string     `json:"slug,omitempty" gorm:"column:slug;type:varchar(60)"`
	Icon        string     `json:"icon,omitempty" gorm:"column:icon;type:varchar(500)"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty" gorm:"column:parent_id;type:uuid"`
	Sort        int        `json:"sort" gorm:"column:sort;default:0"`
	CircleCount int        `json:"circle_count" gorm:"column:circle_count;default:0"`
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`
	Deleted     int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`
	CreateTime  time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名（与旧 model.Category 保持一致，共用 domains.category schema）。
func (Category) TableName() string {
	return "domains.category"
}

// CategoryStatus 分类状态常量。
const (
	CategoryStatusDisabled = 0 // 禁用/隐藏
	CategoryStatusEnabled  = 1 // 启用/显示
)
