package infrastructure

import (
	"context"

	"interestBar/pkg/domains/history/domain"
	"interestBar/pkg/logger"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// historyEventPublisherRedpanda 基于 redpanda 的 HistoryEventPublisher 实现。
type historyEventPublisherRedpanda struct{}

// NewHistoryEventPublisher 构造 HistoryEventPublisher。
func NewHistoryEventPublisher() domain.HistoryEventPublisher {
	return &historyEventPublisherRedpanda{}
}

// PublishPostView 发布帖子浏览历史事件 + CF 互动（浏览，隐式弱信号）。
func (p *historyEventPublisherRedpanda) PublishPostView(ctx context.Context, userID, postID uuid.UUID) error {
	_ = ctx // redpanda 包用 context.Background()(与 collect/like publisher 实现一致)
	if err := redpanda.PublishPostViewHistoryEvent(userID, postID); err != nil {
		return err
	}
	// CF 互动：浏览即写（weight=1，max-ever，不覆盖更强的赞/藏信号）。
	if err := redpanda.PublishPostInteraction(userID, postID, redpanda.InteractionView, redpanda.InteractionWeightView); err != nil {
		logger.Log.Error("Failed to publish post_view interaction: " + err.Error())
	}
	return nil
}
