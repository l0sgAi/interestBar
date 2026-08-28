// Package domain 存放 post 领域的纯领域模型。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// PostMention 帖子 @提及 关系实体（发帖时的最终提及名单，append-only）。
//
// 写入前名单已由 application 层校验：存在且未注销、去重、剔除作者本人、上限截断；
// 消息中心通知只对该名单发。不设 deleted：提及行随内容生灭。
type PostMention struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	PostID     uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (PostMention) TableName() string { return "domains.post_mention" }
