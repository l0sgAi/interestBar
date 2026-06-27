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

func (c *postHistoryCacheRedis) ListViews(ctx context.Context, userID uuid.UUID, offset, size int) ([]domain.ViewEntry, int64, error) {
	_ = ctx
	raw, total, err := redispkg.ListPostViews(userID, int64(offset), int64(size))
	if err != nil {
		return nil, 0, err
	}
	entries := make([]domain.ViewEntry, 0, len(raw))
	for _, e := range raw {
		id, perr := uuid.Parse(e.ID)
		if perr != nil {
			continue
		}
		entries = append(entries, domain.ViewEntry{PostID: id, ViewedAt: e.ViewedAt})
	}
	return entries, total, nil
}

func (c *postHistoryCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, entries []domain.ViewEntry) error {
	_ = ctx
	raw := make([]redispkg.PostViewEntry, 0, len(entries))
	for _, e := range entries {
		raw = append(raw, redispkg.PostViewEntry{ID: e.PostID.String(), ViewedAt: e.ViewedAt})
	}
	return redispkg.BackfillPostViews(userID, raw)
}
