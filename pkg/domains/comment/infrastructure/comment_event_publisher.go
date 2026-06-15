package infrastructure

import (
	"context"

	"interestBar/pkg/domains/comment/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// commentEventPublisherRedpanda 基于 redpanda 的 CommentEventPublisher 实现。
type commentEventPublisherRedpanda struct{}

// NewCommentEventPublisher 构造 CommentEventPublisher。
func NewCommentEventPublisher() domain.CommentEventPublisher {
	return &commentEventPublisherRedpanda{}
}

// PublishPostCommentCount 发布帖子评论数变化事件。
func (p *commentEventPublisherRedpanda) PublishPostCommentCount(ctx context.Context, postID uuid.UUID, delta int64) error {
	return redpanda.PublishPostCommentCount(postID, delta)
}
