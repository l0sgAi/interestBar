package domain

import (
	"context"

	"github.com/google/uuid"
)

// CommentRepository 是 comment 领域的持久化接口。
type CommentRepository interface {
	// Create 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）。
	Create(ctx context.Context, comment *Comment) error
	// GetByID 根据 ID 获取评论（未删除）。未找到返回 ErrCommentNotFound。
	GetByID(ctx context.Context, commentID uuid.UUID) (*Comment, error)
	// GetRootCommentsByCursor 游标分页获取帖子的顶层评论。
	// sort: 0=按点赞倒序, 1=按时间倒序。
	// 返回评论列表、下一页游标、是否有更多、错误。
	GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]Comment, string, bool, error)
	// GetRepliesByCursor 游标分页获取某条评论的子回复。
	// sort: 0=按时间倒序, 1=按点赞倒序。
	GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]Comment, string, bool, error)
	// IsLiked 检查用户是否点赞了评论（DB 回源用）。
	IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error)
	// BatchCheckLiked 批量检查用户是否点赞了多条评论（DB 回源用）。
	BatchCheckLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error)
}

// CommentStatsCache 评论统计信息缓存（like_count）。
type CommentStatsCache interface {
	// Exists 检查统计 Hash 是否存在。
	Exists(ctx context.Context, commentID uuid.UUID) (bool, error)
	// Set 设置统计信息（用于从 DB 恢复）。
	Set(ctx context.Context, commentID uuid.UUID, likeCount int) error
}

// CommentLikeCache 评论点赞状态缓存（Redis ZSET）。
type CommentLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多条评论。
	// 返回：已点赞的 map（未命中的 key 值为 false）、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error
}

// CommentEventPublisher 评论事件发布（异步累积帖子热度）。
type CommentEventPublisher interface {
	// PublishCommentHot 发布评论对帖子热度的贡献。
	// dir: +1 创建评论；-1 删除评论（TODO: 删除评论功能未实现，预留）。
	// 权重与 per-post 上限（cap.comment）由源头 Lua 原子 clamp，本方法只发布最终 Δ。
	PublishCommentHot(ctx context.Context, postID uuid.UUID, dir int) error
}
