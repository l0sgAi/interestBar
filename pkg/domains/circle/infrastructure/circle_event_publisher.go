package infrastructure

import (
	"context"

	"interestBar/pkg/domains/circle/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// circleEventPublisherRedpanda 基于 pkg/server/storage/redpanda 的 CircleEventPublisher 实现。
type circleEventPublisherRedpanda struct{}

// NewCircleEventPublisher 构造 CircleEventPublisher。
func NewCircleEventPublisher() domain.CircleEventPublisher {
	return &circleEventPublisherRedpanda{}
}

// PublishMemberCount 发布成员计数变化消息（+1/-1）。
func (p *circleEventPublisherRedpanda) PublishMemberCount(ctx context.Context, circleID uuid.UUID, delta int64) error {
	return redpanda.PublishCircleMemberCount(circleID, delta)
}

// PublishPostCount 发布帖子计数 +1 消息。
func (p *circleEventPublisherRedpanda) PublishPostCount(ctx context.Context, circleID uuid.UUID) error {
	return redpanda.PublishCirclePostCount(circleID)
}
