package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostLikeCache 帖子点赞缓存（Redis ZSET + stats Hash + Lua 原子切换）。
//
// like 领域用这个接口完成"帖子点赞"的原子切换（ZSET 增删 + stats Hash 增减）。
type PostLikeCache interface {
	// Toggle 原子切换帖子点赞状态。
	Toggle(ctx context.Context, userID, postID uuid.UUID) (ToggleResult, error)
	// StatsExists 检查帖子统计 Hash 是否存在（用于恢复缓存）。
	StatsExists(ctx context.Context, postID uuid.UUID) (bool, error)
}

// CommentLikeCache 评论点赞缓存（Redis ZSET + stats Hash + Lua 原子切换）。
type CommentLikeCache interface {
	// Toggle 原子切换评论点赞状态。
	Toggle(ctx context.Context, userID, commentID uuid.UUID) (ToggleResult, error)
	// StatsExists 检查评论统计 Hash 是否存在（用于恢复缓存）。
	StatsExists(ctx context.Context, commentID uuid.UUID) (bool, error)
}

// LikeEventPublisher 点赞事件发布（异步持久化到 DB）。
type LikeEventPublisher interface {
	// PublishPostLike 发布帖子点赞事件。
	// amount: 1=点赞, -1=取消点赞。
	PublishPostLike(ctx context.Context, userID, postID uuid.UUID, amount int64) error
	// PublishCommentLike 发布评论点赞事件。
	// postID 是冗余字段（评论所属帖子ID），用于消费者更新 comment.post_id。
	PublishCommentLike(ctx context.Context, userID, commentID, postID uuid.UUID, amount int64) error
}
