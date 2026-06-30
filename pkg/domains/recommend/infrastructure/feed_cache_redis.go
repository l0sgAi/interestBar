package infrastructure

import (
	"context"
	"time"

	"interestBar/pkg/domains/recommend/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// feedCacheRedis 基于 redispkg feed:recommend:* 的 FeedCache 实现。
type feedCacheRedis struct{}

// NewFeedCache 构造 FeedCache。
func NewFeedCache() domain.FeedCache {
	return &feedCacheRedis{}
}

func (c *feedCacheRedis) Exists(ctx context.Context, userID uuid.UUID) (bool, error) {
	_ = ctx
	return redispkg.FeedPoolExists(userID)
}

func (c *feedCacheRedis) Len(ctx context.Context, userID uuid.UUID) (int64, error) {
	_ = ctx
	return redispkg.FeedPoolLen(userID)
}

func (c *feedCacheRedis) Range(ctx context.Context, userID uuid.UUID, offset, size int64) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.FeedPoolRange(userID, offset, size)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, perr := uuid.Parse(s); perr == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *feedCacheRedis) Token(ctx context.Context, userID uuid.UUID) (string, error) {
	_ = ctx
	return redispkg.GetFeedPoolToken(userID)
}

func (c *feedCacheRedis) Build(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, ttl time.Duration) (string, error) {
	_ = ctx
	strs := make([]string, 0, len(ids))
	for _, id := range ids {
		strs = append(strs, id.String())
	}
	return redispkg.BuildFeedPool(userID, strs, ttl)
}
