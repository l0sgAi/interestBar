// Package application 提供 like 领域的应用服务层。
//
// 职责：
//   - 点赞/取消点赞（幂等的 Toggle 操作，Redis Lua 原子切换）
//   - 跨 post / comment 两种目标类型
//   - 点赞前恢复统计缓存（避免 Lua 脚本读到不存在的 stats Hash）
//   - 异步发布点赞事件（Redpanda → 消费者批量持久化到 DB）
package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/like/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// PostTarget like 领域需要的帖子查询能力。
type PostTarget interface {
	// Exists 检查帖子是否存在（未删除）。存在返回 true，不存在返回 false。
	Exists(ctx context.Context, postID uuid.UUID) (bool, error)
	// RestoreStats 恢复帖子统计缓存（如果不存在）。
	// 用于点赞前确保 Redis stats Hash 存在，避免 Lua 脚本读到空 stats。
	RestoreStats(ctx context.Context, postID uuid.UUID) error
}

// CommentTarget like 领域需要的评论查询能力。
type CommentTarget interface {
	// ExistsWithPostID 检查评论是否存在，并返回其所属帖子ID。
	// 未找到返回 nil, false, nil。
	ExistsWithPostID(ctx context.Context, commentID uuid.UUID) (postID *uuid.UUID, exists bool, err error)
	// RestoreStats 恢复评论统计缓存（如果不存在）。
	RestoreStats(ctx context.Context, commentID uuid.UUID) error
}

// ===== DTO =====

// ToggleResult 点赞切换结果（供 handler 序列化）。
type ToggleResult struct {
	IsLiked  bool   `json:"is_liked"`
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
}

// ToggleInput 点赞/取消点赞入参。
type ToggleInput struct {
	Type     string    // "comment" 或 "post"
	TargetID uuid.UUID // 评论ID 或 帖子ID
}

// ===== Service 接口 =====

// LikeService 是 like 领域的应用服务接口。
type LikeService interface {
	// Toggle 点赞/取消点赞（幂等操作）。
	Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error)

	// SetPostTarget 注入帖子查询端口。
	SetPostTarget(t PostTarget)
	// SetCommentTarget 注入评论查询端口。
	SetCommentTarget(t CommentTarget)
}

type likeServiceImpl struct {
	postCache    domain.PostLikeCache
	commentCache domain.CommentLikeCache
	publisher    domain.LikeEventPublisher
	postTarget   PostTarget
	commentTarget CommentTarget
}

// NewLikeService 构造 LikeService。
//
// postTarget / commentTarget 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewLikeService(
	postCache domain.PostLikeCache,
	commentCache domain.CommentLikeCache,
	publisher domain.LikeEventPublisher,
) LikeService {
	return &likeServiceImpl{
		postCache:    postCache,
		commentCache: commentCache,
		publisher:    publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *likeServiceImpl) SetPostTarget(t PostTarget)       { s.postTarget = t }
func (s *likeServiceImpl) SetCommentTarget(t CommentTarget) { s.commentTarget = t }

// Toggle 点赞/取消点赞（幂等操作）。
//
// 与旧 controller.ToggleLike 行为一致：
//  1. 根据 type 分支：comment 需查评论存在性 + 拿 postID + 恢复评论统计缓存；
//     post 需查帖子存在性 + 恢复帖子统计缓存。
//  2. 执行 Redis Lua 原子切换（ZSET 增删 + stats Hash 增减）。
//  3. 发布 Redpanda 点赞事件（异步持久化到 DB）。
func (s *likeServiceImpl) Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error) {
	switch domain.TargetType(input.Type) {
	case domain.TargetTypeComment:
		return s.toggleCommentLike(ctx, userID, input.TargetID)
	case domain.TargetTypePost:
		return s.togglePostLike(ctx, userID, input.TargetID)
	default:
		return nil, domain.ErrInvalidTargetType
	}
}

// togglePostLike 帖子点赞切换。
func (s *likeServiceImpl) togglePostLike(ctx context.Context, userID, postID uuid.UUID) (*ToggleResult, error) {
	if s.postTarget == nil {
		return nil, errors.New("post target is not configured")
	}

	// 1. 校验帖子存在
	exists, err := s.postTarget.Exists(ctx, postID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrPostNotFound
	}

	// 2. 确保帖子统计缓存存在（Lua 脚本依赖 stats Hash）
	if err := s.postTarget.RestoreStats(ctx, postID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}

	// 3. 原子切换点赞状态
	result, err := s.postCache.Toggle(ctx, userID, postID)
	if err != nil {
		return nil, fmt.Errorf("failed to toggle post like: %w", err)
	}

	// 4. 发布点赞事件（异步持久化）
	if err := s.publisher.PublishPostLike(ctx, userID, postID, result.Int64()); err != nil {
		logger.Log.Error("Failed to publish post like event: " + err.Error())
	}

	return &ToggleResult{
		IsLiked:  result == domain.ToggleResultLiked,
		Type:     string(domain.TargetTypePost),
		TargetID: postID.String(),
	}, nil
}

// toggleCommentLike 评论点赞切换。
func (s *likeServiceImpl) toggleCommentLike(ctx context.Context, userID, commentID uuid.UUID) (*ToggleResult, error) {
	if s.commentTarget == nil {
		return nil, errors.New("comment target is not configured")
	}

	// 1. 校验评论存在 + 拿到冗余 postID
	postIDPtr, exists, err := s.commentTarget.ExistsWithPostID(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrCommentNotFound
	}

	// 2. 确保评论统计缓存存在（Lua 脚本依赖 stats Hash）
	if err := s.commentTarget.RestoreStats(ctx, commentID); err != nil {
		logger.Log.Error("Failed to restore comment stats cache: " + err.Error())
	}

	// 3. 原子切换点赞状态
	result, err := s.commentCache.Toggle(ctx, userID, commentID)
	if err != nil {
		return nil, fmt.Errorf("failed to toggle comment like: %w", err)
	}

	// 4. 发布点赞事件（异步持久化）
	var postID uuid.UUID
	if postIDPtr != nil {
		postID = *postIDPtr
	}
	if err := s.publisher.PublishCommentLike(ctx, userID, commentID, postID, result.Int64()); err != nil {
		logger.Log.Error("Failed to publish comment like event: " + err.Error())
	}

	return &ToggleResult{
		IsLiked:  result == domain.ToggleResultLiked,
		Type:     string(domain.TargetTypeComment),
		TargetID: commentID.String(),
	}, nil
}
