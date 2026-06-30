// Package domain 存放 history 领域的纯领域模型。
//
// history 领域是「帖子浏览历史」用例的聚合点(仅针对 post)。
// 它管理「最近浏览」的记录(Redis ZSET 即时读) + 异步落库(Redpanda → consumer upsert)。
// post_view_history 流水表也由本领域持有(由 MQ consumer 写入,本领域 repo 仅提供冷启动回源读)。
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// PostViewHistory 帖子浏览历史实体(表 domains.post_view_history)。
//
// 去重模型:每对 (user_id, post_id) 仅一行,再看时 bump update_time + view_count。
// 写入由 redpanda history_consumer 批量 ON CONFLICT upsert 完成(非本领域 repo),
// 本领域 repo 仅提供冷启动回源读(ListTopByUserID)。
type PostViewHistory struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	PostID     uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	ViewCount  int       `json:"view_count" gorm:"column:view_count;default:1"`
	CreateTime time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名。
func (PostViewHistory) TableName() string { return "domains.post_view_history" }

// 哨兵错误。
var (
	// ErrInvalidCursor 列表游标/分页参数非法。
	ErrInvalidCursor = errors.New("invalid pagination parameter")
)
