package infrastructure

import (
	"context"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	redispkg "interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"

	"github.com/google/uuid"
)

// commentEventPublisherRedpanda 基于 redpanda 的 CommentEventPublisher 实现。
type commentEventPublisherRedpanda struct{}

// NewCommentEventPublisher 构造 CommentEventPublisher。
func NewCommentEventPublisher() domain.CommentEventPublisher {
	return &commentEventPublisherRedpanda{}
}

// PublishCommentHot 累积评论对帖子热度的贡献（+5，per-post 上限 cap.comment，Lua 原子 clamp）。
func (p *commentEventPublisherRedpanda) PublishCommentHot(ctx context.Context, postID uuid.UUID, dir int) error {
	delta, err := redispkg.ApplyHotDelta(postID, redispkg.HotDimComment, dir)
	if err != nil {
		logger.Log.Error("Failed to apply comment hot delta: " + err.Error())
		return nil // best-effort：不阻断发评论主流程
	}
	if delta == 0 {
		return nil // 已达 cap 上限，无增量
	}
	if err := redpanda.PublishPostHot(postID, delta); err != nil {
		logger.Log.Error("Failed to publish comment hot: " + err.Error())
	}
	return nil
}

// PublishCommentInteraction 发布评论者对帖子的 CF 互动（weight=comment，正向写矩阵）。
func (p *commentEventPublisherRedpanda) PublishCommentInteraction(ctx context.Context, userID, postID uuid.UUID) error {
	_ = ctx
	if err := redpanda.PublishPostInteraction(userID, postID, redpanda.InteractionComment, redpanda.InteractionWeightComment); err != nil {
		logger.Log.Error("Failed to publish comment interaction: " + err.Error())
	}
	return nil
}

// PublishCommentNotice 发布评论通知事件（消息中心，best-effort 不阻断主流程）。
func (p *commentEventPublisherRedpanda) PublishCommentNotice(ctx context.Context, userID, postID, commentID uuid.UUID, isReply bool, snippet string) error {
	_ = ctx
	noticeType := redpanda.NoticeTypeCommentPost
	if isReply {
		noticeType = redpanda.NoticeTypeReplyComment
	}
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:      noticeType,
		ActorID:   userID,
		PostID:    &postID,
		CommentID: &commentID,
		Snippet:   snippet,
	}); err != nil {
		logger.Log.Error("Failed to publish comment notification: " + err.Error())
	}
	return nil
}

// PublishMentionNotice 发布 @提及 通知事件（消息中心，best-effort 不阻断主流程）。
func (p *commentEventPublisherRedpanda) PublishMentionNotice(ctx context.Context, actorID uuid.UUID, postID, commentID *uuid.UUID, mentionUserIDs []uuid.UUID, snippet string) error {
	_ = ctx
	if len(mentionUserIDs) == 0 {
		return nil
	}
	if err := redpanda.PublishNotificationEvent(redpanda.NotificationEventMessage{
		Type:           redpanda.NoticeTypeMention,
		ActorID:        actorID,
		PostID:         postID,
		CommentID:      commentID,
		MentionUserIDs: mentionUserIDs,
		Snippet:        snippet,
	}); err != nil {
		logger.Log.Error("Failed to publish mention notification: " + err.Error())
	}
	return nil
}
