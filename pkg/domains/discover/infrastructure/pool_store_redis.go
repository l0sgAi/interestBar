package infrastructure

import (
	"context"
	"time"

	"github.com/google/uuid"

	"interestBar/pkg/domains/discover/domain"
	redispkg "interestBar/pkg/server/storage/redis"
)

// poolStoreRedis 基于 redispkg discover:* 的 DiscoverPoolStore 实现（无状态，委托全局 Client）。
//
// 与 recommend.feedCacheRedis 同款风格：薄适配器，把 string↔uuid 转换收口在此。
type poolStoreRedis struct{}

// NewDiscoverPoolStore 构造 DiscoverPoolStore。
func NewDiscoverPoolStore() domain.DiscoverPoolStore {
	return &poolStoreRedis{}
}

func (s *poolStoreRedis) Range(ctx context.Context, section, userKey string, offset, size int64) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.DiscoverPoolRange(userKey, section, offset, size)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, str := range raw {
		if id, perr := uuid.Parse(str); perr == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *poolStoreRedis) Token(ctx context.Context, userKey string) (string, error) {
	_ = ctx
	return redispkg.GetDiscoverPoolToken(userKey)
}

func (s *poolStoreRedis) Exists(ctx context.Context, section, userKey string) (bool, error) {
	_ = ctx
	return redispkg.DiscoverPoolExists(userKey, section)
}

func (s *poolStoreRedis) Len(ctx context.Context, section, userKey string) (int64, error) {
	_ = ctx
	return redispkg.DiscoverPoolLen(userKey, section)
}

func (s *poolStoreRedis) Rebuild(ctx context.Context, section, userKey string, ids []uuid.UUID, ttl time.Duration) (string, error) {
	_ = ctx
	strs := make([]string, 0, len(ids))
	for _, id := range ids {
		strs = append(strs, id.String())
	}
	return redispkg.BuildDiscoverPool(userKey, section, strs, ttl)
}
