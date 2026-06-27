package domain

import (
	"context"

	"github.com/google/uuid"
)

// HistoryRecorder 浏览历史记录端口。
//
// 供 post 领域在帖子详情页浏览时(asyncIncrementView)回调,由 composition
// 注入 history 领域实现(对称 collect 的 PostTarget / PostFetcher 注入)。
type HistoryRecorder interface {
	// RecordView 记录一次浏览(幂等:再看 bump update_time + view_count)。
	// 实现侧:Redis ZSET 即时写 + Redpanda 异步落库。失败仅记日志,不影响详情接口。
	RecordView(ctx context.Context, userID, postID uuid.UUID) error
}
