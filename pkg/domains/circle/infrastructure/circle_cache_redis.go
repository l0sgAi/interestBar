package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// circleBaseCacheTTL 圈子基础信息缓存有效期（与旧 controller 一致：24 小时）。
const circleBaseCacheTTL = 24 * time.Hour

// circleBaseCacheRedis 基于 Redis 的 CircleBaseCache 实现（压缩存储）。
type circleBaseCacheRedis struct{}

// NewCircleBaseCache 构造 CircleBaseCache。
func NewCircleBaseCache() domain.CircleBaseCache {
	return &circleBaseCacheRedis{}
}

func (c *circleBaseCacheRedis) GetBase(ctx context.Context, circleID uuid.UUID) (*domain.CircleBaseInfo, error) {
	key := redispkg.GetCircleInfoKey(circleID)
	var info domain.CircleBaseInfo
	if err := redispkg.GetJSONCompressed(key, &info); err != nil {
		// 缓存 miss 不告警；其他错误（连接失败/反序列化失败）记日志便于观测
		if !errors.Is(err, redis.Nil) {
			logger.Log.Warn("Redis cache error for circle base info: " + err.Error())
		}
		return nil, nil
	}
	return &info, nil
}

func (c *circleBaseCacheRedis) SetBase(ctx context.Context, circleID uuid.UUID, info *domain.CircleBaseInfo) error {
	key := redispkg.GetCircleInfoKey(circleID)
	return redispkg.SetJSONCompressed(key, info, circleBaseCacheTTL)
}

// circleStatsCacheRedis 基于 Redis Hash 的 CircleStatsCache 实现。
//
// 复用 redispkg 的 CircleStatistics 类型与原子操作函数，保持与旧 controller
// incrementCircleMemberCount / decrementCircleMemberCount / getCircleStatistics 行为一致。
type circleStatsCacheRedis struct{}

// NewCircleStatsCache 构造 CircleStatsCache。
func NewCircleStatsCache() domain.CircleStatsCache {
	return &circleStatsCacheRedis{}
}

func (c *circleStatsCacheRedis) GetStats(ctx context.Context, circleID uuid.UUID) (*domain.CircleStatistics, error) {
	stats, err := redispkg.GetCircleStatistics(circleID)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		return nil, nil
	}
	return &domain.CircleStatistics{
		MemberCount: stats.MemberCount,
		PostCount:   stats.PostCount,
		Hot:         stats.Hot,
	}, nil
}

func (c *circleStatsCacheRedis) StatsExists(ctx context.Context, circleID uuid.UUID) (bool, error) {
	return redispkg.CircleStatisticsExists(circleID)
}

func (c *circleStatsCacheRedis) SetStats(ctx context.Context, circleID uuid.UUID, stats *domain.CircleStatistics) error {
	return redispkg.UpdateCircleStatistics(circleID, &redispkg.CircleStatistics{
		MemberCount: stats.MemberCount,
		PostCount:   stats.PostCount,
		Hot:         stats.Hot,
	})
}

func (c *circleStatsCacheRedis) IncrMemberCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.IncrementCircleMemberCount(circleID)
}

func (c *circleStatsCacheRedis) DecrMemberCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.DecrementCircleMemberCount(circleID)
}

func (c *circleStatsCacheRedis) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	return redispkg.IncrementCirclePostCount(circleID)
}

// joinedCirclesCacheTTL 用户加入圈子列表缓存有效期（与旧 controller 一致：24 小时）。
const joinedCirclesCacheTTL = 24 * time.Hour

// joinedCirclesCacheRedis 基于 Redis 的 JoinedCirclesCache 实现。
type joinedCirclesCacheRedis struct{}

// NewJoinedCirclesCache 构造 JoinedCirclesCache。
func NewJoinedCirclesCache() domain.JoinedCirclesCache {
	return &joinedCirclesCacheRedis{}
}

func (c *joinedCirclesCacheRedis) GetJoined(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	key := redispkg.GetUserJoinedCirclesKey(userID)
	var circleIDs []uuid.UUID
	if err := redispkg.GetJSON(key, &circleIDs); err != nil {
		// 缓存 miss 不告警；其他错误记日志
		if !errors.Is(err, redis.Nil) {
			logger.Log.Warn("Redis cache error for joined circles: " + err.Error())
		}
		return nil, nil
	}
	return circleIDs, nil
}

func (c *joinedCirclesCacheRedis) SetJoined(ctx context.Context, userID uuid.UUID, circleIDs []uuid.UUID) error {
	key := redispkg.GetUserJoinedCirclesKey(userID)
	return redispkg.SetJSON(key, circleIDs, joinedCirclesCacheTTL)
}

func (c *joinedCirclesCacheRedis) InvalidateJoined(ctx context.Context, userID uuid.UUID) error {
	key := redispkg.GetUserJoinedCirclesKey(userID)
	return redispkg.Del(key)
}
