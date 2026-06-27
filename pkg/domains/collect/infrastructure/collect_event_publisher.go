package infrastructure

import (
	"context"

	"interestBar/pkg/domains/collect/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// collectEventPublisherRedpanda 基于 redpanda 的 CollectEventPublisher 实现。
type collectEventPublisherRedpanda struct{}

// NewCollectEventPublisher 构造 CollectEventPublisher。
func NewCollectEventPublisher() domain.CollectEventPublisher {
	return &collectEventPublisherRedpanda{}
}

// PublishPostCollect 发布帖子收藏事件。
func (p *collectEventPublisherRedpanda) PublishPostCollect(ctx context.Context, userID, postID uuid.UUID, amount int64) error {
	return redpanda.PublishPostCollectEvent(userID, postID, amount)
}
