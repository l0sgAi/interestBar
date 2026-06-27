package infrastructure

import (
	"context"

	"interestBar/pkg/domains/collect/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// collectEventPublisherRedpanda 基于 redpanda 的 CollectEventPublisher 实现。
type collectEventPublisherRedpanda struct{}

// NewCollectEventPublisher 构造 CollectEventPublisher。
func NewCollectEventPublisher() domain.CollectEventPublisher {
	return &collectEventPublisherRedpanda{}
}

// PublishPostCollect 发布帖子收藏事件 + 累积帖子热度。
func (p *collectEventPublisherRedpanda) PublishPostCollect(ctx context.Context, userID, postID uuid.UUID, amount int64) error {
	if err := redpanda.PublishPostCollectEvent(userID, postID, amount); err != nil {
		return err
	}
	// 热度：收藏 ±weight，无上限（一人一收藏）。best-effort，失败仅告警不阻断主流程。
	dir := 1
	if amount <= 0 {
		dir = -1
	}
	if delta, err := redispkg.ApplyHotDelta(postID, redispkg.HotDimPostCollect, dir); err != nil {
		logger.Log.Error("Failed to apply post_collect hot delta: " + err.Error())
	} else if delta != 0 {
		if err := redpanda.PublishPostHot(postID, delta); err != nil {
			logger.Log.Error("Failed to publish post_collect hot: " + err.Error())
		}
	}
	return nil
}
