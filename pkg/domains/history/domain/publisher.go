package domain

import (
	"context"

	"github.com/google/uuid"
)

// HistoryEventPublisher 浏览历史事件发布(异步聚合落库到 DB)。
//
// RecordView 写 Redis ZSET 后发布本事件;redpanda history_consumer 批量
// ON CONFLICT upsert 到 post_view_history 表。MQ 失败仅影响 DB 最终一致,
// 不影响 Redis 即时读(下次浏览补偿)。
type HistoryEventPublisher interface {
	// PublishPostView 发布帖子浏览事件。
	PublishPostView(ctx context.Context, userID, postID uuid.UUID) error
}
