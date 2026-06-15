package infrastructure

import (
	"context"

	"interestBar/pkg/domains/post/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// postEventPublisherRedpanda 基于 redpanda 的 PostEventPublisher 实现。
type postEventPublisherRedpanda struct{}

// NewPostEventPublisher 构造 PostEventPublisher。
func NewPostEventPublisher() domain.PostEventPublisher {
	return &postEventPublisherRedpanda{}
}

// PublishViewCount 发布浏览量变化事件。
func (p *postEventPublisherRedpanda) PublishViewCount(ctx context.Context, postID uuid.UUID) error {
	return redpanda.PublishPostViewCount(postID)
}

// PublishCommentCount 发布帖子评论数变化事件。
func (p *postEventPublisherRedpanda) PublishCommentCount(ctx context.Context, postID uuid.UUID, delta int64) error {
	return redpanda.PublishPostCommentCount(postID, delta)
}
