package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostHistoryCache 帖子浏览历史缓存(Redis ZSET,即时读 + cap 500 去重)。
//
// ZSET key: user:view:posts:{user_id},score=访问时间(Unix 毫秒),member=postId。
// 列表读即 ZREVRANGE(最近访问倒序);cap 500 超限按最低 score 淘汰。
type PostHistoryCache interface {
	// RecordView 原子记录一次浏览(ZADD upsert + trim 500 + EXPIRE, Lua 脚本)。
	// 重复浏览同帖则 bump score 移到「最近浏览」顶部。
	RecordView(ctx context.Context, userID, postID uuid.UUID) error
	// ListViews 倒序取浏览历史 [offset, offset+size-1] 区间 postID(最近访问倒序)。
	// 返回 postID 列表、总数(ZCARD)、error。total==0 触发上层冷启动回源。
	ListViews(ctx context.Context, userID uuid.UUID, offset, size int) (postIDs []uuid.UUID, total int64, err error)
	// Backfill DB 回源后批量回填 ZSET(冷启动恢复)。
	// postIDs 须已按 update_time DESC 排序(由 repo 保证),回填后 ZSET 序与 DB 序一致。
	Backfill(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) error
}
