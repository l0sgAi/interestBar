// Package domain 是共享内核（Shared Kernel）：跨领域复用的基础领域构件。
//
// 这里只放"所有领域都公认共享、且修改需全领域一致"的极小集合。
// 放进来的东西越少越好——共享内核是耦合点，应保持克制。
//
// 目前只包含：
//   - BaseModel：所有 GORM 实体的公共主键 + 时间戳字段。
//
// 注意命名冲突：本包名为 domain（pkg/shared/domain），各领域下也有
// domain 子包（如 pkg/domains/category/domain）。Go 的包导入路径
// 是全局唯一的，不会冲突；但 import 时会用别名区分，例如：
//
//	categoryDomain "interestBar/pkg/domains/category/domain"
//	sharedDomain   "interestBar/pkg/shared/domain"
package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BaseModel 公共字段：UUIDv7 主键 + 创建/更新时间。
// 所有业务表内嵌此结构，统一主键生成与时间戳维护。
//
// 设计说明：
//   - 主键使用 UUIDv7（前 48 位为毫秒级时间戳），字典序 == 时间序，
//     可直接用于「最新优先」排序与 keyset 游标分页。
//   - 主键在应用层（GORM）的 BeforeCreate 钩子中生成，而非依赖 DB 默认值：
//     GORM 会把 uuid.UUID 的零值（nil UUID）当作有效值发送，从而覆盖 DB 的
//     `DEFAULT uuidv7()`。在 Go 端预生成可避免该问题，并保证 Create 后 ID 立即可用
//     （例如 CreateCircle 在插入 circle 后需立即读取 circle.ID 去创建成员记录）。
//   - DB 列上的 `DEFAULT uuidv7()` 仅为手工 SQL 插入时的兜底。
type BaseModel struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// BeforeCreate 在插入前为未设置主键的记录生成 UUIDv7。
// 仅当 ID 为零值（uuid.Nil）时生成，允许外部显式指定（如种子数据）。
func (b *BaseModel) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.Must(uuid.NewV7())
	}
	return nil
}
