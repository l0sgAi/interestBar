package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PostRepository 是 post 领域的持久化接口。
type PostRepository interface {
	// GetByID 根据 ID 获取帖子（不限制 status）。未找到返回 ErrPostNotFound。
	GetByID(ctx context.Context, postID uuid.UUID) (*Post, error)
	// Create 创建帖子。
	Create(ctx context.Context, post *Post) error
	// GetMediaByPostIDs 批量获取帖子的图片列表（仅查 id + media_extra）。
	GetMediaByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]MediaExtraJSON, error)
	// IsLiked 检查用户是否点赞了帖子（DB 回源用）。
	IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error)
}

// PostStatsCache 帖子统计信息缓存（view_count/comment_count/like_count 等）。
type PostStatsCache interface {
	// Exists 检查统计 Hash 是否存在。
	Exists(ctx context.Context, postID uuid.UUID) (bool, error)
	// Get 获取统计信息。未命中返回 nil, nil。
	Get(ctx context.Context, postID uuid.UUID) (*PostStatistics, error)
	// Set 设置统计信息（用于从 DB 恢复）。
	Set(ctx context.Context, postID uuid.UUID, stats *PostStatistics) error
	// IncrViewCount 增加浏览量（带去重），返回新计数值。
	IncrViewCount(ctx context.Context, postID, userID uuid.UUID) (int64, error)
	// IncrCommentCount 递增帖子评论计数（Hash 字段 comment_count +1）。
	// 供 comment 领域发评论后调用。
	IncrCommentCount(ctx context.Context, postID uuid.UUID) error
}

// PostLikeCache 帖子点赞状态缓存（Redis ZSET）。
type PostLikeCache interface {
	// BatchCheck 批量检查用户是否点赞了多个帖子。
	// 返回：已点赞的 map、未命中的 postID 列表、error。
	BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked map[uuid.UUID]bool, missed []uuid.UUID, err error)
	// Backfill 回填 DB 查询确认的点赞状态到 ZSET。
	Backfill(ctx context.Context, userID uuid.UUID, likedPostIDs []uuid.UUID) error
}

// PostEventPublisher 帖子事件发布（异步持久化统计到 DB）。
type PostEventPublisher interface {
	// PublishViewCount 发布浏览量变化事件。
	PublishViewCount(ctx context.Context, postID uuid.UUID) error
	// PublishCommentCount 发布帖子评论数变化事件。
	// delta: 1=新评论, -1=删除评论。
	PublishCommentCount(ctx context.Context, postID uuid.UUID, delta int64) error
}

// PostStatistics 帖子统计信息。
type PostStatistics struct {
	ViewCount    int `json:"view_count"`
	CommentCount int `json:"comment_count"`
	LikeCount    int `json:"like_count"`
	CollectCount int `json:"collect_count"`
}

// PostMediaFetcherError 保留：未来可在领域层定义更细的错误类型。
var ErrMediaFetchFailed = errors.New("failed to fetch post media")
