package infrastructure

import (
	"context"

	"interestBar/pkg/domains/comment/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// commentStatsCacheRedis 基于 Redis Hash 的 CommentStatsCache 实现。
type commentStatsCacheRedis struct{}

// NewCommentStatsCache 构造 CommentStatsCache。
func NewCommentStatsCache() domain.CommentStatsCache {
	return &commentStatsCacheRedis{}
}

func (c *commentStatsCacheRedis) Exists(ctx context.Context, commentID uuid.UUID) (bool, error) {
	return redispkg.CommentStatisticsExists(commentID)
}

func (c *commentStatsCacheRedis) Set(ctx context.Context, commentID uuid.UUID, likeCount int) error {
	return redispkg.UpdateCommentStatistics(commentID, likeCount)
}

// commentLikeCacheRedis 基于 Redis ZSET 的 CommentLikeCache 实现。
type commentLikeCacheRedis struct{}

// NewCommentLikeCache 构造 CommentLikeCache。
func NewCommentLikeCache() domain.CommentLikeCache {
	return &commentLikeCacheRedis{}
}

func (c *commentLikeCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	return redispkg.BatchCheckCommentLiked(userID, commentIDs)
}

func (c *commentLikeCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, likedCommentIDs []uuid.UUID) error {
	return redispkg.BackfillCommentLikes(userID, likedCommentIDs)
}
