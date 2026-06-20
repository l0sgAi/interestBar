// Package infrastructure 提供 like 领域基础设施层实现。
package infrastructure

import (
	"context"

	"interestBar/pkg/domains/like/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postLikeCacheRedis 基于 Redis 的 PostLikeCache 实现。
//
// 复用 pkg/server/storage/redis 中已有的 Lua 原子切换脚本和 stats 操作。
type postLikeCacheRedis struct{}

// NewPostLikeCache 构造 PostLikeCache。
func NewPostLikeCache() domain.PostLikeCache {
	return &postLikeCacheRedis{}
}

func (c *postLikeCacheRedis) Toggle(ctx context.Context, userID, postID uuid.UUID) (domain.ToggleResult, error) {
	r, err := redispkg.TogglePostLike(userID, postID)
	if err != nil {
		return 0, err
	}
	return domain.ToggleResult(r), nil
}

func (c *postLikeCacheRedis) StatsExists(ctx context.Context, postID uuid.UUID) (bool, error) {
	return redispkg.PostStatisticsExists(postID)
}

// commentLikeCacheRedis 基于 Redis 的 CommentLikeCache 实现。
type commentLikeCacheRedis struct{}

// NewCommentLikeCache 构造 CommentLikeCache。
func NewCommentLikeCache() domain.CommentLikeCache {
	return &commentLikeCacheRedis{}
}

func (c *commentLikeCacheRedis) Toggle(ctx context.Context, userID, commentID uuid.UUID) (domain.ToggleResult, error) {
	r, err := redispkg.ToggleCommentLike(userID, commentID)
	if err != nil {
		return 0, err
	}
	return domain.ToggleResult(r), nil
}

func (c *commentLikeCacheRedis) StatsExists(ctx context.Context, commentID uuid.UUID) (bool, error) {
	return redispkg.CommentStatisticsExists(commentID)
}
