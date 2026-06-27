package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostHistoryRepository post_view_history 流水表读接口。
//
// 注意:流水写入由 redpanda history_consumer 批量 ON CONFLICT upsert 完成,
// 不经本接口。本接口仅提供冷启动回源读(ZSET 过期后从 DB top500 恢复)。
type PostHistoryRepository interface {
	// ListTopByUserID 按 update_time DESC 取该用户最近 size 条浏览历史的 postID。
	// 供冷启动回源:ZCARD==0 时从 DB 恢复 top500 回填 ZSET。
	ListTopByUserID(ctx context.Context, userID uuid.UUID, size int) ([]uuid.UUID, error)
}
