// Package application 提供 recommend 领域的应用服务层（推荐流编排）。
package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"interestBar/pkg/conf"
	"interestBar/pkg/domains/recommend/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
)

// ErrTabNotSupported 请求的 tab 暂未实现（仅 recommend）。
var ErrTabNotSupported = errors.New("home feed tab not supported")

// RecommendService 推荐流应用服务。
type RecommendService interface {
	// GetHomeFeed 首页信息流分页（按 tab 分发）。
	//   tab=recommend  → 候选池 offset 翻页（用 poolToken）
	//   tab=hot|latest  → ES search_after 翻页（用 searchAfter），全局
	//   tab=following   → ES search_after 翻页（用 searchAfter），限定已加入圈子
	GetHomeFeed(ctx context.Context, userID uuid.UUID, tab string, size, offset int, poolToken string, searchAfter []interface{}) (*domain.FeedPage, error)
}

type recommendServiceImpl struct {
	searcher domain.HomeFeedSearcher
	circle   domain.CircleLookup
	postMeta domain.PostMetaReader
	seed     domain.SeedReader
	hydrator domain.PostHydrator
	checker  domain.InteractionChecker
	feed     domain.FeedCache
	intCache domain.InterestCircleCache
}

// NewRecommendService 构造 RecommendService。
//
// searcher/seed/checker/feed/intCache 为 recommend 同域 infra（直构）；
// circle/postMeta/hydrator 为跨域桥接器（composition 注入）。
func NewRecommendService(
	searcher domain.HomeFeedSearcher,
	circle domain.CircleLookup,
	postMeta domain.PostMetaReader,
	seed domain.SeedReader,
	hydrator domain.PostHydrator,
	checker domain.InteractionChecker,
	feed domain.FeedCache,
	intCache domain.InterestCircleCache,
) RecommendService {
	return &recommendServiceImpl{
		searcher: searcher,
		circle:   circle,
		postMeta: postMeta,
		seed:     seed,
		hydrator: hydrator,
		checker:  checker,
		feed:     feed,
		intCache: intCache,
	}
}

// GetHomeFeed 按 tab 分发：recommend 走候选池 offset；hot/latest/following 走 ES search_after。
func (s *recommendServiceImpl) GetHomeFeed(ctx context.Context, userID uuid.UUID, tab string, size, offset int, poolToken string, searchAfter []interface{}) (*domain.FeedPage, error) {
	switch tab {
	case "recommend":
		return s.getRecommend(ctx, userID, size, offset, poolToken)
	case "hot":
		return s.getSimpleFeed(ctx, userID, "hot", nil, size, searchAfter)
	case "latest":
		return s.getSimpleFeed(ctx, userID, "latest", nil, size, searchAfter)
	case "following":
		return s.getFollowing(ctx, userID, size, searchAfter)
	default:
		return nil, ErrTabNotSupported
	}
}

// getRecommend 推荐流（候选池 offset 翻页）。编排：确保池 → LRANGE → hydrate → 补交互态。
func (s *recommendServiceImpl) getRecommend(ctx context.Context, userID uuid.UUID, size, offset int, poolToken string) (*domain.FeedPage, error) {
	if size <= 0 || size > 100 {
		size = 20
	}
	if offset < 0 {
		offset = 0
	}

	feedCfg := conf.Config.Recommend.Feed
	poolTTL := time.Duration(defaultInt(feedCfg.TTLMinutes, 30)) * time.Minute

	// 1. 确保候选池
	exists, err := s.feed.Exists(ctx, userID)
	if err != nil {
		logger.Log.Error("feed exists check: " + err.Error())
	}
	currentToken, _ := s.feed.Token(ctx, userID)
	poolRefreshed := false

	switch {
	case !exists:
		// 池 miss → 重建
		currentToken = s.rebuildPool(ctx, userID, poolTTL)
	case poolToken != "" && poolToken != currentToken:
		// 客户端 token 过期（池自上次后已被重建）→ 重建 + 回 offset=0，防翻页错位
		currentToken = s.rebuildPool(ctx, userID, poolTTL)
		offset = 0
		poolRefreshed = true
	}

	// 2. 池长度 + offset 边界
	total, err := s.feed.Len(ctx, userID)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return &domain.FeedPage{Posts: []domain.FeedPostItem{}, PoolToken: currentToken, HasMore: false, PoolRefreshed: poolRefreshed}, nil
	}
	if int64(offset) >= total {
		return &domain.FeedPage{Posts: []domain.FeedPostItem{}, PoolToken: currentToken, HasMore: false, PoolRefreshed: poolRefreshed}, nil
	}

	// 3. 取本页 IDs + hydrate（按 LRANGE 序重排，因 ES terms 不保序）
	ids, err := s.feed.Range(ctx, userID, int64(offset), int64(size))
	if err != nil {
		return nil, err
	}
	posts := s.hydrateOrdered(ctx, ids)

	// 4. 补交互态 is_liked/is_collected
	if len(posts) > 0 {
		liked, collected, _ := s.checker.BatchCheck(ctx, userID, ids)
		for i := range posts {
			posts[i].IsLiked = liked[posts[i].ID]
			posts[i].IsCollected = collected[posts[i].ID]
		}
	}

	hasMore := int64(offset+size) < total
	return &domain.FeedPage{
		Posts:         posts,
		PoolToken:     currentToken,
		HasMore:       hasMore,
		PoolRefreshed: poolRefreshed,
	}, nil
}

// rebuildPool 跑 5 路召回 + 合并，落候选池，返回新 token。
func (s *recommendServiceImpl) rebuildPool(ctx context.Context, userID uuid.UUID, ttl time.Duration) string {
	ids := s.recallAll(ctx, userID)
	token, err := s.feed.Build(ctx, userID, ids, ttl)
	if err != nil {
		logger.Log.Error("rebuild feed pool: " + err.Error())
		return ""
	}
	logger.Log.Debug("feed pool rebuilt")
	return token
}

// hydrateOrdered 按 ids 顺序 hydrate（Hydrate 内部 ES terms 不保序，需按入参序重排）。
func (s *recommendServiceImpl) hydrateOrdered(ctx context.Context, ids []uuid.UUID) []domain.FeedPostItem {
	if len(ids) == 0 {
		return []domain.FeedPostItem{}
	}
	items, err := s.hydrator.Hydrate(ctx, ids)
	if err != nil {
		logger.Log.Error("hydrate feed posts: " + err.Error())
		return []domain.FeedPostItem{}
	}
	byID := make(map[uuid.UUID]domain.FeedPostItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make([]domain.FeedPostItem, 0, len(ids))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}
	return out
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// getSimpleFeed hot/latest 简单流（ES search_after 翻页，可选 circle 过滤）。
// 单次 ES 检索 → 提取 ID → hydrate → 补交互态。非个性化，无候选池。
func (s *recommendServiceImpl) getSimpleFeed(ctx context.Context, userID uuid.UUID, sort string, circleIDs []uuid.UUID, size int, searchAfter []interface{}) (*domain.FeedPage, error) {
	if size <= 0 || size > 100 {
		size = 20
	}
	ids, next, err := s.searcher.Search(ctx, sort, circleIDs, size, searchAfter)
	if err != nil {
		return nil, err
	}
	posts := s.hydrateOrdered(ctx, ids)
	if len(posts) > 0 {
		liked, collected, _ := s.checker.BatchCheck(ctx, userID, ids)
		for i := range posts {
			posts[i].IsLiked = liked[posts[i].ID]
			posts[i].IsCollected = collected[posts[i].ID]
		}
	}
	return &domain.FeedPage{
		Posts:       posts,
		SearchAfter: marshalSearchAfter(next),
		HasMore:     len(ids) == size, // 满页才可能有更多
	}, nil
}

// getFollowing 关注流（已加入圈子，按发帖时间倒序）。
// 项目无 user-follow，关注 = 已加圈子（唯一可行映射）。0 圈子 → 空列表。
func (s *recommendServiceImpl) getFollowing(ctx context.Context, userID uuid.UUID, size int, searchAfter []interface{}) (*domain.FeedPage, error) {
	circles := s.safeJoined(ctx, userID, 200) // 关注流取更多圈子
	if len(circles) == 0 {
		return &domain.FeedPage{Posts: []domain.FeedPostItem{}, HasMore: false}, nil
	}
	return s.getSimpleFeed(ctx, userID, "latest", circles, size, searchAfter)
}

// marshalSearchAfter 把 ES sort values 序列化为不透明 JSON 字符串（search_after 游标）。
func marshalSearchAfter(arr []interface{}) string {
	if len(arr) == 0 {
		return ""
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return ""
	}
	return string(b)
}
