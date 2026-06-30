package infrastructure

import (
	"context"

	"interestBar/pkg/domains/post/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// postStatsCacheRedis 基于 Redis Hash 的 PostStatsCache 实现。
type postStatsCacheRedis struct{}

// NewPostStatsCache 构造 PostStatsCache。
func NewPostStatsCache() domain.PostStatsCache {
	return &postStatsCacheRedis{}
}

func (c *postStatsCacheRedis) Exists(ctx context.Context, postID uuid.UUID) (bool, error) {
	return redispkg.PostStatisticsExists(postID)
}

func (c *postStatsCacheRedis) Get(ctx context.Context, postID uuid.UUID) (*domain.PostStatistics, error) {
	stats, err := redispkg.GetPostStatistics(postID)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		return nil, nil
	}
	return &domain.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	}, nil
}

func (c *postStatsCacheRedis) Set(ctx context.Context, postID uuid.UUID, stats *domain.PostStatistics) error {
	return redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	})
}

func (c *postStatsCacheRedis) BatchGet(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]*domain.PostStatistics, error) {
	raw, err := redispkg.GetPostStatisticsBatch(postIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]*domain.PostStatistics, len(raw))
	for pid, st := range raw {
		result[pid] = &domain.PostStatistics{
			ViewCount:    st.ViewCount,
			CommentCount: st.CommentCount,
			LikeCount:    st.LikeCount,
			CollectCount: st.CollectCount,
		}
	}
	return result, nil
}

func (c *postStatsCacheRedis) SetIfAbsent(ctx context.Context, postID uuid.UUID, stats *domain.PostStatistics) error {
	return redispkg.SeedPostStatisticsIfAbsent(postID, &redispkg.PostStatistics{
		ViewCount:    stats.ViewCount,
		CommentCount: stats.CommentCount,
		LikeCount:    stats.LikeCount,
		CollectCount: stats.CollectCount,
	})
}

func (c *postStatsCacheRedis) IncrViewCount(ctx context.Context, postID, userID uuid.UUID) (int64, error) {
	return redispkg.IncrementPostViewCount(postID, userID)
}

func (c *postStatsCacheRedis) IncrCommentCount(ctx context.Context, postID uuid.UUID) error {
	return redispkg.IncrementPostCommentCount(postID)
}

// postLikeCacheRedis 基于 Redis ZSET 的 PostLikeCache 实现。
type postLikeCacheRedis struct{}

// NewPostLikeCache 构造 PostLikeCache。
func NewPostLikeCache() domain.PostLikeCache {
	return &postLikeCacheRedis{}
}

func (c *postLikeCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	return redispkg.BatchCheckPostLiked(userID, postIDs)
}

func (c *postLikeCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, likedPostIDs []uuid.UUID) error {
	return redispkg.BackfillPostLikes(userID, likedPostIDs)
}

// postCollectCacheRedis 基于 Redis ZSET 的 PostCollectCache 实现（只读 BatchCheck/Backfill）。
type postCollectCacheRedis struct{}

// NewPostCollectCache 构造 PostCollectCache。
func NewPostCollectCache() domain.PostCollectCache {
	return &postCollectCacheRedis{}
}

func (c *postCollectCacheRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, []uuid.UUID, error) {
	return redispkg.BatchCheckPostCollected(userID, postIDs)
}

func (c *postCollectCacheRedis) Backfill(ctx context.Context, userID uuid.UUID, collectedPostIDs []uuid.UUID) error {
	return redispkg.BackfillPostCollects(userID, collectedPostIDs)
}
