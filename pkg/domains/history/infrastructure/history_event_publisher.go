package infrastructure

import (
	"context"

	"interestBar/pkg/domains/history/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// historyEventPublisherRedpanda 基于 redpanda 的 HistoryEventPublisher 实现。
type historyEventPublisherRedpanda struct{}

// NewHistoryEventPublisher 构造 HistoryEventPublisher。
func NewHistoryEventPublisher() domain.HistoryEventPublisher {
	return &historyEventPublisherRedpanda{}
}

// PublishPostView 发布帖子浏览历史事件。
func (p *historyEventPublisherRedpanda) PublishPostView(ctx context.Context, userID, postID uuid.UUID) error {
	_ = ctx // redpanda 包用 context.Background()(与 collect/like publisher 实现一致)
	return redpanda.PublishPostViewHistoryEvent(userID, postID)
}
