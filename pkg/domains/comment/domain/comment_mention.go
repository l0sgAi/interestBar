// Package domain 存放 comment 领域的纯领域模型。
package domain

import (
	"time"

	"github.com/google/uuid"
)

// CommentMention 评论 @提及 关系实体（发评论/回复时的最终提及名单，append-only）。
//
// 写入前名单已由 application 层校验：存在且未注销、去重、剔除评论人本人、上限截断；
// 消息中心通知只对该名单发。不设 deleted：提及行随内容生灭。
type CommentMention struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	CommentID  uuid.UUID `json:"comment_id" gorm:"column:comment_id;type:uuid;not null"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (CommentMention) TableName() string { return "domains.comment_mention" }
