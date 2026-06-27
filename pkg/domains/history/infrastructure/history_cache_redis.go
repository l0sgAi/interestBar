// Package infrastructure 提供 history 领域基础设施层实现。
package infrastructure

import (
	"context"

	"interestBar/pkg/domains/history/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postHistoryCacheRedis 基于 Redis ZSET 的 PostHistoryCache 实现。
//
// 复用 pkg/server/storage/redis 中 history 专用的 Lua 脚本(RecordPostView)和 ZSET 读操作。
type postHistoryCacheRedis struct{}

// NewPostHistoryCache 构造 PostHistoryCache。
func NewPostHistoryCache() domain.PostHistoryCache {
	return &postHistoryCacheRedis{}
}

func (c *postHistoryCacheRedis) RecordView(ctx context.Context, userID, postID uuid.UUID) error {
	_ = ctx // redis 包用全局 ctx(与 collect/like cache 实现一致)
	return redispkg.RecordPostView(userID, postID)
}

func (c *postHistoryCacheRedis) ListViews(ctx context.Context, userID uuid.UUID, offset, size int) ([]uuid.UUID, int64, error) {
	_ = ctx
	members, total, err := redispkg.ListPostViews(userID, int64(offset), int64(size))
	if err != nil {
		return nil, 0, err
	}
	postIDs := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if id, perr := uuid.Parse(m); perr == nil {
			postIDs = append(postIDs, id)
		}
	}
	return postIDs, total, nil
}

func (c *postHistoryCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) error {
	_ = ctx
	return redispkg.BackfillPostViews(userID, postIDs)
}
