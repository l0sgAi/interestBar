// Package application 提供 collect 领域的应用服务层。
//
// 职责：
//   - 收藏/取消收藏（幂等的 Toggle 操作，Redis Lua 原子切换）
//   - 「我的收藏」列表（DB keyset 分页 + 复用 post 领域组装）
//   - 收藏前恢复统计缓存（避免 Lua 脚本读到不存在的 stats Hash）
//   - 收藏流水即时入库（列表权威源，Toggle 同步 upsert post_collect）
//   - 异步发布 collect_count 增量事件（Redpanda → 消费者批量聚合统计字段）
package application

import (
	"context"
	"errors"
	"fmt"

	"interestBar/pkg/domains/collect/domain"
	postapp "interestBar/pkg/domains/post/application"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// PostFetcher collect 领域需要的帖子组装能力（「我的收藏」列表用）。
type PostFetcher interface {
	// GetPostsByIDs 按 ID 列表批量获取已组装的帖子（仅未删除 + 已发布）。
	// 顺序不保证，调用方按收藏时间自行排序；失效帖静默过滤（不在返回中）。
	GetPostsByIDs(ctx context.Context, postIDs []uuid.UUID) ([]postapp.PostListItem, error)
}

// ===== DTO =====

// ToggleResult 收藏切换结果（供 handler 序列化）。
type ToggleResult struct {
	IsCollected bool   `json:"is_collected"`
	PostID      string `json:"post_id"`
}

// ToggleInput 收藏/取消收藏入参。
type ToggleInput struct {
	PostID uuid.UUID
}

// ListCollectedPostsResult 「我的收藏」列表结果。
type ListCollectedPostsResult struct {
	Posts       []postapp.PostListItem `json:"posts"`
	Total       int64                  `json:"total"`
	Size        int                    `json:"size"`
	SearchAfter string                 `json:"search_after"`
}

// ===== Service 接口 =====

// CollectService 是 collect 领域的应用服务接口。
type CollectService interface {
	// Toggle 收藏/取消收藏（幂等操作）。
	Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error)
	// ListCollectedPosts 查看当前用户的收藏列表（按收藏时间倒序）。
	ListCollectedPosts(ctx context.Context, userID uuid.UUID, size int, searchAfter string) (*ListCollectedPostsResult, error)

	// SetPostTarget 注入帖子查询端口（存在性校验 + 统计缓存恢复）。
	SetPostTarget(t domain.PostTarget)
	// SetPostFetcher 注入帖子组装端口（「我的收藏」列表用）。
	SetPostFetcher(f PostFetcher)
}

type collectServiceImpl struct {
	cache       domain.PostCollectCache
	repo        domain.PostCollectRepository
	publisher   domain.CollectEventPublisher
	postTarget  domain.PostTarget
	postFetcher PostFetcher
}

// NewCollectService 构造 CollectService。
//
// postTarget / postFetcher 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewCollectService(
	cache domain.PostCollectCache,
	repo domain.PostCollectRepository,
	publisher domain.CollectEventPublisher,
) CollectService {
	return &collectServiceImpl{
		cache:     cache,
		repo:      repo,
		publisher: publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *collectServiceImpl) SetPostTarget(t domain.PostTarget) { s.postTarget = t }
func (s *collectServiceImpl) SetPostFetcher(f PostFetcher)     { s.postFetcher = f }

// Toggle 收藏/取消收藏（幂等操作）。
//
// 与 like.togglePostLike 的差异：收藏是低频操作且「我的收藏」列表需即时可见，
// 故 post_collect 流水同步入库（步骤 4），仅 collect_count 统计字段走异步聚合（步骤 5）。
//  1. 校验帖子存在；
//  2. 确保帖子统计缓存存在（Lua 脚本依赖 stats Hash）；
//  3. 执行 Redis Lua 原子切换（ZSET 增删 + stats Hash collect_count 增减），返回方向 ±1；
//  4. 同步 upsert post_collect 流水；DB 写失败则反向切回 Redis 补偿（保持 Redis/DB 一致），
//     返回 error 供客户端重试；
//  5. 发布 Redpanda collect_count 增量事件（消费者批量聚合，不再落流水）。
func (s *collectServiceImpl) Toggle(ctx context.Context, userID uuid.UUID, input ToggleInput) (*ToggleResult, error) {
	if s.postTarget == nil {
		return nil, errors.New("post target is not configured")
	}

	// 1. 校验帖子存在
	exists, err := s.postTarget.Exists(ctx, input.PostID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrPostNotFound
	}

	// 2. 确保帖子统计缓存存在（Lua 脚本依赖 stats Hash）
	if err := s.postTarget.RestoreStats(ctx, input.PostID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}

	// 3. 原子切换收藏状态（Redis 权威，返回方向决定 DB upsert 动作）
	result, err := s.cache.Toggle(ctx, userID, input.PostID)
	if err != nil {
		return nil, fmt.Errorf("failed to toggle post collect: %w", err)
	}

	// 4. 同步落库收藏流水（列表权威源，需即时可见）
	active := result == domain.ToggleResultCollected
	if err := s.repo.SetCollected(ctx, userID, input.PostID, active); err != nil {
		// 补偿：反向切回 Redis（不发事件），保持 Redis/DB 一致，供客户端重试
		if _, rbErr := s.cache.Toggle(ctx, userID, input.PostID); rbErr != nil {
			logger.Log.Error("Failed to rollback redis after db write failure: " + rbErr.Error())
		}
		return nil, fmt.Errorf("failed to persist post collect: %w", err)
	}

	// 5. 发布 collect_count 增量事件（异步聚合统计字段，不再落流水）
	if err := s.publisher.PublishPostCollect(ctx, userID, input.PostID, result.Int64()); err != nil {
		logger.Log.Error("Failed to publish post collect event: " + err.Error())
	}

	return &ToggleResult{
		IsCollected: result == domain.ToggleResultCollected,
		PostID:      input.PostID.String(),
	}, nil
}

// ListCollectedPosts 查看当前用户的收藏列表。
//
// 数据源：DB post_collect（deleted=0），按收藏时间倒序 keyset 分页。
// ZSET 仅用于信息流「是否已收藏」回显，不作为列表权威源（有 2000 条上限 + TTL 失活）。
// 失效帖（被删/未发布）在 post 组装时静默过滤（决策 #4）。
func (s *collectServiceImpl) ListCollectedPosts(ctx context.Context, userID uuid.UUID, size int, searchAfter string) (*ListCollectedPostsResult, error) {
	if s.postFetcher == nil {
		return nil, errors.New("post fetcher is not configured")
	}

	if size <= 0 || size > 100 {
		size = 20
	}

	postIDs, total, nextCursor, err := s.repo.ListCollectedPostIDs(ctx, userID, size, searchAfter)
	if err != nil {
		return nil, err
	}

	posts := make([]postapp.PostListItem, 0, len(postIDs))
	if len(postIDs) > 0 {
		fetched, err := s.postFetcher.GetPostsByIDs(ctx, postIDs)
		if err != nil {
			return nil, err
		}
		// 按 postIDs（收藏时间倒序）重排：GetPostsByIDs 不保证顺序，且会过滤失效帖。
		posts = orderByCollectTime(fetched, postIDs)
	}

	return &ListCollectedPostsResult{
		Posts:       posts,
		Total:       total,
		Size:        len(posts),
		SearchAfter: nextCursor,
	}, nil
}

// orderByCollectTime 把 fetched（无序、可能少于 postIDs）按 postIDs 的顺序重排。
func orderByCollectTime(fetched []postapp.PostListItem, orderedIDs []uuid.UUID) []postapp.PostListItem {
	byID := make(map[uuid.UUID]postapp.PostListItem, len(fetched))
	for _, p := range fetched {
		byID[p.ID] = p
	}
	result := make([]postapp.PostListItem, 0, len(fetched))
	for _, id := range orderedIDs {
		if p, ok := byID[id]; ok {
			result = append(result, p)
		}
	}
	return result
}
