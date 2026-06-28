package infrastructure

import (
	"context"

	"interestBar/pkg/domains/recommend/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
)

// interactionCheckerRedis 基于 redispkg 的 InteractionChecker 实现（user:like/collect ZSET 批量查）。
type interactionCheckerRedis struct{}

// NewInteractionChecker 构造 InteractionChecker。
func NewInteractionChecker() domain.InteractionChecker {
	return &interactionCheckerRedis{}
}

// BatchCheck 批量查 is_liked/is_collected。缓存 miss（未命中 ZSET）按 false 处理（保守，不影响推荐展示）。
func (c *interactionCheckerRedis) BatchCheck(ctx context.Context, userID uuid.UUID, postIDs []uuid.UUID) (liked, collected map[uuid.UUID]bool, err error) {
	_ = ctx
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]bool), make(map[uuid.UUID]bool), nil
	}
	liked, _, err = redispkg.BatchCheckPostLiked(userID, postIDs)
	if err != nil {
		return nil, nil, err
	}
	collected, _, err = redispkg.BatchCheckPostCollected(userID, postIDs)
	if err != nil {
		return nil, nil, err
	}
	return liked, collected, nil
}
