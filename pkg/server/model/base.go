package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BaseModel 公共字段:UUIDv7 主键 + 创建/更新时间。
// 所有业务表内嵌此结构,统一主键生成与时间戳维护。
//
// 设计说明:
//   - 主键使用 UUIDv7(前 48 位为毫秒级时间戳),字典序 == 时间序,
//     可直接用于「最新优先」排序与 keyset 游标分页。
//   - 主键在应用层(GORM)的 BeforeCreate 钩子中生成,而非依赖 DB 默认值:
//     GORM 会把 uuid.UUID 的零值(nil UUID)当作有效值发送,从而覆盖 DB 的
//     `DEFAULT uuidv7()`。在 Go 端预生成可避免该问题,并保证 Create 后 ID 立即可用
//     (例如 CreateCircle 在插入 circle 后需立即读取 circle.ID 去创建成员记录)。
//   - DB 列上的 `DEFAULT uuidv7()` 仅为手工 SQL 插入时的兜底。
type BaseModel struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// BeforeCreate 在插入前为未设置主键的记录生成 UUIDv7。
// 仅当 ID 为零值(uuid.Nil)时生成,允许外部显式指定(如种子数据)。
func (b *BaseModel) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.Must(uuid.NewV7())
	}
	return nil
}
