package infrastructure

import (
	"context"

	"interestBar/pkg/domains/like/domain"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// likeEventPublisherRedpanda 基于 redpanda 的 LikeEventPublisher 实现。
type likeEventPublisherRedpanda struct{}

// NewLikeEventPublisher 构造 LikeEventPublisher。
func NewLikeEventPublisher() domain.LikeEventPublisher {
	return &likeEventPublisherRedpanda{}
}

// PublishPostLike 发布帖子点赞事件。
func (p *likeEventPublisherRedpanda) PublishPostLike(ctx context.Context, userID, postID uuid.UUID, amount int64) error {
	return redpanda.PublishPostLikeEvent(userID, postID, amount)
}

// PublishCommentLike 发布评论点赞事件。
func (p *likeEventPublisherRedpanda) PublishCommentLike(ctx context.Context, userID, commentID, postID uuid.UUID, amount int64) error {
	return redpanda.PublishCommentLikeEvent(userID, commentID, postID, amount)
}
