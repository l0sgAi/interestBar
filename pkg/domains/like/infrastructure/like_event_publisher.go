package infrastructure

import (
	"context"

	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// likeEventPublisherRedpanda 基于 redpanda 的 LikeEventPublisher 实现。
type likeEventPublisherRedpanda struct{}

// NewLikeEventPublisher 构造 LikeEventPublisher。
func NewLikeEventPublisher() domain.LikeEventPublisher {
	return &likeEventPublisherRedpanda{}
}

// hotDirection 由 toggle 的 amount(±1) 推导 hot 方向：+1 增加 / -1 撤销。
func hotDirection(amount int64) int {
	if amount > 0 {
		return 1
	}
	return -1
}

// PublishPostLike 发布帖子点赞事件 + 累积帖子热度。
func (p *likeEventPublisherRedpanda) PublishPostLike(ctx context.Context, userID, postID uuid.UUID, amount int64) error {
	if err := redpanda.PublishPostLikeEvent(userID, postID, amount); err != nil {
		return err
	}
	// 热度：点赞 ±weight，无上限（一人一赞）。best-effort，失败仅告警不阻断主流程。
	if delta, err := redispkg.ApplyHotDelta(postID, redispkg.HotDimPostLike, hotDirection(amount)); err != nil {
		logger.Log.Error("Failed to apply post_like hot delta: " + err.Error())
	} else if delta != 0 {
		if err := redpanda.PublishPostHot(postID, delta); err != nil {
			logger.Log.Error("Failed to publish post_like hot: " + err.Error())
		}
	}
	// CF 互动：仅正向（点赞）写互动矩阵；取消赞不删行（隐反馈），故负向不发。
	if amount > 0 {
		if err := redpanda.PublishPostInteraction(userID, postID, redpanda.InteractionLike, redpanda.InteractionWeightLike); err != nil {
			logger.Log.Error("Failed to publish post_like interaction: " + err.Error())
		}
		// 通知：帖子被赞 → 帖子作者（接收人由 consumer 反查）。负向不通知不回收。
		if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
			Type:    redpanda.NoticeTypeLikePost,
			ActorID: userID,
			PostID:  &postID,
		}); err != nil {
			logger.Log.Error("Failed to publish post_like notification: " + err.Error())
		}
	}
	return nil
}

// PublishCommentLike 发布评论点赞事件 + 累积帖子热度。
func (p *likeEventPublisherRedpanda) PublishCommentLike(ctx context.Context, userID, commentID, postID uuid.UUID, amount int64) error {
	if err := redpanda.PublishCommentLikeEvent(userID, commentID, postID, amount); err != nil {
		return err
	}
	// 热度：评论点赞 ±1，per-post 上限 cap.comment_like（Lua 原子 clamp）。热度归属帖子。
	if delta, err := redispkg.ApplyHotDelta(postID, redispkg.HotDimCommentLike, hotDirection(amount)); err != nil {
		logger.Log.Error("Failed to apply comment_like hot delta: " + err.Error())
	} else if delta != 0 {
		if err := redpanda.PublishPostHot(postID, delta); err != nil {
			logger.Log.Error("Failed to publish comment_like hot: " + err.Error())
		}
	}
	// CF 互动：仅正向（评论点赞）写互动矩阵。
	if amount > 0 {
		if err := redpanda.PublishPostInteraction(userID, postID, redpanda.InteractionCommentLike, redpanda.InteractionWeightCommentLike); err != nil {
			logger.Log.Error("Failed to publish comment_like interaction: " + err.Error())
		}
		// 通知：评论被赞 → 评论作者（接收人由 consumer 反查）。负向不通知不回收。
		if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
			Type:      redpanda.NoticeTypeLikeComment,
			ActorID:   userID,
			PostID:    &postID,
			CommentID: &commentID,
		}); err != nil {
			logger.Log.Error("Failed to publish comment_like notification: " + err.Error())
		}
	}
	return nil
}
