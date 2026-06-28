package infrastructure

import (
	"context"
	"time"

	"interestBar/pkg/domains/recommend/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// interestCircleCacheRedis 基于 redispkg user:interest_circles:* 的 InterestCircleCache 实现。
type interestCircleCacheRedis struct{}

// NewInterestCircleCache 构造 InterestCircleCache。
func NewInterestCircleCache() domain.InterestCircleCache {
	return &interestCircleCacheRedis{}
}

func (c *interestCircleCacheRedis) Exists(ctx context.Context, userID uuid.UUID) (bool, error) {
	_ = ctx
	return redispkg.InterestCirclesExists(userID)
}

func (c *interestCircleCacheRedis) Get(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.GetInterestCircles(userID)
	if err != nil {
		return nil, err
	}
	return parseIDs(raw), nil
}

func (c *interestCircleCacheRedis) Set(ctx context.Context, userID uuid.UUID, circleIDs []uuid.UUID, ttl time.Duration) error {
	_ = ctx
	return redispkg.SetInterestCircles(userID, circleIDs, ttl)
}
