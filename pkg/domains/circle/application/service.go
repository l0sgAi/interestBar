// Package application 提供 circle 领域的应用服务层。
//
// 职责：
//   - 圈子 CRUD + 加入/退出 + 成员状态校验
//   - 统计计数管理（Redis 实时 + DB 恢复 + Redpanda 异步持久化）
//   - 圈内帖子列表组装（调用 post Facade 与 user Facade）
//   - 通过 CircleFacade 向 post 领域暴露"圈子精简视图"查询
package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ===== Facade（跨领域视图）=====

// CircleBrief 是给跨领域调用的圈子精简视图。
type CircleBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// CircleFacade 是 circle 领域给 post 等领域的跨领域查询接口。
type CircleFacade interface {
	// GetBriefs 批量获取圈子精简视图。
	GetBriefs(ctx context.Context, circleIDs []string) (map[string]CircleBrief, error)
}

// ===== 跨领域依赖（post/user Facade，由 composition 注入）=====

// UserBrief 用户精简视图（与 user.application.UserBrief 字段一致，但独立定义避免跨领域 import）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserFacade 是 circle 领域需要的 user 查询接口（由 composition 注入 user 领域实现）。
type UserFacade interface {
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
}

// PostMediaFetcher 帖子媒体批量查询接口（由 composition 注入 post 领域实现）。
//
// circle 领域的 GetCirclePosts 需要批量查询帖子图片列表，
// 但帖子实体属于 post 领域。通过此接口解耦。
type PostMediaFetcher interface {
	// GetMediaByPostIDs 批量获取帖子的图片 URL 列表。
	GetMediaByPostIDs(ctx context.Context, postIDs []string) (map[string][]string, error)
}

// ===== 搜索结果 DTO =====

// CircleDoc 圈子搜索结果项（对应 ES CircleDocument）。
type CircleDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description"`
	Hot         int    `json:"hot"`
	CategoryID  string `json:"category_id"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreateTime  string `json:"create_time"`
	Status      int16  `json:"status"`
	JoinType    int16  `json:"join_type"`
}

// CircleSearchResult 圈子列表搜索结果。
type CircleSearchResult struct {
	Circles     []CircleDoc  `json:"circles"`
	Total       int64        `json:"total"`
	Size        int          `json:"size"`
	SearchAfter string       `json:"search_after"`
}

// MyCircleDoc 我加入的圈子结果项。
type MyCircleDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	MemberCount int    `json:"member_count"`
}

// MyCircleSearchResult 我加入圈子搜索结果。
type MyCircleSearchResult struct {
	Circles     []MyCircleDoc `json:"circles"`
	Total       int64         `json:"total"`
	Size        int           `json:"size"`
	SearchAfter string        `json:"search_after"`
}

// PostDoc 圈内帖子搜索结果项（ES PostDocument 的精简版）。
type PostDoc struct {
	ID           string `json:"id"`
	CircleID     string `json:"circle_id"`
	UserID       string `json:"user_id"`
	Type         int16  `json:"type"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	Content      string `json:"content"`
	ViewCount    int    `json:"view_count"`
	CommentCount int    `json:"comment_count"`
	LikeCount    int    `json:"like_count"`
	CollectCount int    `json:"collect_count"`
	IsPinned     int16  `json:"is_pinned"`
	IsEssence    int16  `json:"is_essence"`
	IsLock       int16  `json:"is_lock"`
	Status       int16  `json:"status"`
	CreateTime   string `json:"create_time"`
}

// CirclePostListItem 组装后的圈内帖子项（含作者/圈子信息/图片）。
type CirclePostListItem struct {
	ID           uuid.UUID `json:"id"`
	CircleID     uuid.UUID `json:"circle_id"`
	UserID       uuid.UUID `json:"user_id"`
	Type         int16     `json:"type"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Content      string    `json:"content"`
	ViewCount    int       `json:"view_count"`
	CommentCount int       `json:"comment_count"`
	LikeCount    int       `json:"like_count"`
	CollectCount int       `json:"collect_count"`
	IsPinned     int16     `json:"is_pinned"`
	IsEssence    int16     `json:"is_essence"`
	IsLock       int16     `json:"is_lock"`
	Status       int16     `json:"status"`
	CreateTime   time.Time `json:"create_time"`
	AuthorName   string    `json:"author_name"`
	AuthorAvatar string    `json:"author_avatar"`
	CircleName   string    `json:"circle_name"`
	CircleAvatar string    `json:"circle_avatar"`
	Images       []string  `json:"images"`
}

// CirclePostResult 圈内帖子搜索结果（含组装后的帖子列表，返回给 HTTP 层）。
type CirclePostResult struct {
	Posts       []CirclePostListItem `json:"posts"`
	Total       int64                `json:"total"`
	Size        int                  `json:"size"`
	SearchAfter string               `json:"search_after"`
}

// RawCirclePostResult 是 searcher 返回的原始 ES 结果（未组装）。
type RawCirclePostResult struct {
	Posts       []PostDoc
	Total       int64
	Size        int
	SearchAfter string
}

// ===== 用例输入/输出 DTO =====

// CreateCircleInput 创建圈子入参。
type CreateCircleInput struct {
	Name        string
	Slug        string
	AvatarURL   string
	CoverURL    string
	Description string
	Rule        string
	CategoryID  uuid.UUID
	JoinType    int16
}

// CircleDetailVO 圈子详情（含用户成员信息）。
type CircleDetailVO struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	Hot         int        `json:"hot"`
	MemberCount int        `json:"member_count"`
	PostCount   int        `json:"post_count"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`

	IsJoined          bool       `json:"is_joined"`
	MemberRole        int16      `json:"member_role,omitempty"`
	MemberStatus      int16      `json:"member_status,omitempty"`
	MemberMuteEndTime *time.Time `json:"member_mute_end_time,omitempty"`
	MemberIsTop       int16      `json:"member_is_top,omitempty"`
	MemberIsDisturb   int16      `json:"member_is_disturb,omitempty"`
}

// CircleSearcher 圈子搜索抽象（由 infrastructure 提供 ES 实现）。
type CircleSearcher interface {
	Search(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error)
	SearchMy(ctx context.Context, circleIDs []uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error)
	SearchCirclePosts(ctx context.Context, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*RawCirclePostResult, error)
}

// CircleService 是 circle 领域的应用服务接口。
type CircleService interface {
	CreateCircle(ctx context.Context, userID uuid.UUID, input CreateCircleInput) error
	GetCircleDetail(ctx context.Context, userID, circleID uuid.UUID) (*CircleDetailVO, error)
	JoinCircle(ctx context.Context, userID, circleID uuid.UUID) (joined bool, pending bool, err error)
	LeaveCircle(ctx context.Context, userID, circleID uuid.UUID) error
	SearchCircles(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error)
	GetMyCircles(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error)
	// GetUserCircles 获取任意用户加入的圈子列表（查看「他人」加入的圈子）。
	// 逻辑与 GetMyCircles 一致，仅 userID 来源不同（query 参数 vs 当前会话）。
	GetUserCircles(ctx context.Context, targetUserID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error)
	GetCirclePosts(ctx context.Context, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*CirclePostResult, error)

	// SetUserFacade 注入 user Facade（GetCirclePosts 组装作者信息用）。
	SetUserFacade(f UserFacade)
	// SetPostFetcher 注入 post 媒体查询器（GetCirclePosts 组装图片用）。
	SetPostFetcher(f PostMediaFetcher)
	// IncrPostCount 发帖后递增圈子帖子计数（供 post 领域通过端口调用）。
	IncrPostCount(ctx context.Context, circleID uuid.UUID) error
}

type circleServiceImpl struct {
	repo       domain.CircleRepository
	memberRepo domain.MemberRepository
	baseCache  domain.CircleBaseCache
	statsCache domain.CircleStatsCache
	joinedCache domain.JoinedCirclesCache
	searcher   CircleSearcher
	publisher  domain.CircleEventPublisher
	userFacade UserFacade         // 可为 nil（GetCirclePosts 用）
	postFetcher PostMediaFetcher  // 可为 nil（GetCirclePosts 用）
}

// NewCircleService 构造 CircleService。
//
// userFacade 和 postFetcher 是可选依赖（仅 GetCirclePosts 需要）。
// 调用方应通过 WithUserFacade / WithPostFetcher 方法注入。
func NewCircleService(
	repo domain.CircleRepository,
	memberRepo domain.MemberRepository,
	baseCache domain.CircleBaseCache,
	statsCache domain.CircleStatsCache,
	joinedCache domain.JoinedCirclesCache,
	searcher CircleSearcher,
	publisher domain.CircleEventPublisher,
) CircleService {
	return &circleServiceImpl{
		repo:        repo,
		memberRepo:  memberRepo,
		baseCache:   baseCache,
		statsCache:  statsCache,
		joinedCache: joinedCache,
		searcher:    searcher,
		publisher:   publisher,
	}
}

// NewCircleFacade 从 CircleService 构造 CircleFacade。
func NewCircleFacade(repo domain.CircleRepository) CircleFacade {
	return &circleFacadeAdapter{repo: repo}
}

// circleFacadeAdapter 把 CircleRepository 适配为 CircleFacade。
type circleFacadeAdapter struct {
	repo domain.CircleRepository
}

// GetBriefs 批量获取圈子精简视图。
func (f *circleFacadeAdapter) GetBriefs(ctx context.Context, circleIDs []string) (map[string]CircleBrief, error) {
	if len(circleIDs) == 0 {
		return make(map[string]CircleBrief), nil
	}
	ids := make([]uuid.UUID, 0, len(circleIDs))
	for _, s := range circleIDs {
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return make(map[string]CircleBrief), nil
	}
	circles, err := f.repo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[string]CircleBrief, len(circles))
	for id, c := range circles {
		result[id.String()] = CircleBrief{
			ID:        c.ID.String(),
			Name:      c.Name,
			AvatarURL: c.AvatarURL,
		}
	}
	return result, nil
}

// SetUserFacade 注入 user Facade（composition 层在装配后调用）。
func (s *circleServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }

// SetPostFetcher 注入 post 媒体查询器。
func (s *circleServiceImpl) SetPostFetcher(f PostMediaFetcher) { s.postFetcher = f }

// CreateCircle 创建圈子。
func (s *circleServiceImpl) CreateCircle(ctx context.Context, userID uuid.UUID, input CreateCircleInput) error {
	// 校验 join_type
	if input.JoinType < 0 || input.JoinType > 2 {
		return errInvalidJoinType
	}

	// 检查名称是否已存在
	exists, err := s.repo.ExistsByName(ctx, input.Name)
	if err != nil {
		return err
	}
	if exists {
		return errCircleNameExists
	}

	// 检查 slug 是否已存在
	if input.Slug != "" {
		slug := strings.TrimSpace(input.Slug)
		exists, err := s.repo.ExistsBySlug(ctx, slug)
		if err != nil {
			return err
		}
		if exists {
			return errCircleSlugExists
		}
	}

	categoryID := input.CategoryID
	circle := &domain.Circle{
		Name:        strings.TrimSpace(input.Name),
		Slug:        strings.TrimSpace(input.Slug),
		AvatarURL:   input.AvatarURL,
		CoverURL:    input.CoverURL,
		Description: strings.TrimSpace(input.Description),
		Rule:        strings.TrimSpace(input.Rule),
		CreatorID:   userID,
		CategoryID:  &categoryID,
		Hot:         0,
		MemberCount: 1,
		PostCount:   0,
		JoinType:    input.JoinType,
		Status:      domain.CircleStatusNormal,
		Deleted:     0,
	}

	if err := s.repo.Create(ctx, circle); err != nil {
		return err
	}

	// repo.Create 在事务内把创建者设为圈主成员（status=normal），创建者的已加入圈子列表
	// 已变化，清除旁路缓存，避免 /circle/my 浏览模式读到旧列表。
	if err := s.joinedCache.InvalidateJoined(ctx, userID); err != nil {
		logger.Log.Error("Failed to delete user joined circles cache: " + err.Error())
	}
	return nil
}

// GetCircleDetail 获取圈子详情（缓存优先 + 统计 + 成员信息）。
func (s *circleServiceImpl) GetCircleDetail(ctx context.Context, userID, circleID uuid.UUID) (*CircleDetailVO, error) {
	// 1. 基础信息缓存优先
	base, _ := s.baseCache.GetBase(ctx, circleID)
	if base == nil {
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			return nil, err
		}
		base = &domain.CircleBaseInfo{
			ID: circle.ID, Name: circle.Name, Slug: circle.Slug,
			AvatarURL: circle.AvatarURL, CoverURL: circle.CoverURL,
			Description: circle.Description, Rule: circle.Rule,
			CreatorID: circle.CreatorID, CategoryID: circle.CategoryID,
			JoinType: circle.JoinType, Status: circle.Status, Deleted: circle.Deleted,
			CreateTime: circle.CreateTime, UpdateTime: circle.UpdateTime,
		}
		if err := s.baseCache.SetBase(ctx, circleID, base); err != nil {
			logger.Log.Error("Failed to cache circle base info: " + err.Error())
		}
	}

	// 2. 统计信息（缓存 + DB 恢复）
	memberCount, postCount, hot := s.getStatsWithRecovery(ctx, circleID)

	// 3. 成员信息
	vo := &CircleDetailVO{
		ID: base.ID, Name: base.Name, Slug: base.Slug,
		AvatarURL: base.AvatarURL, CoverURL: base.CoverURL,
		Description: base.Description, Rule: base.Rule,
		CreatorID: base.CreatorID, CategoryID: base.CategoryID,
		Hot: hot, MemberCount: memberCount, PostCount: postCount,
		JoinType: base.JoinType, Status: base.Status, Deleted: base.Deleted,
		CreateTime: base.CreateTime, UpdateTime: base.UpdateTime,
		IsJoined: false,
	}

	member, err := s.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil && err != domain.ErrMemberNotFound {
		return nil, err
	}
	if member != nil {
		vo.IsJoined = true
		vo.MemberRole = member.Role
		vo.MemberStatus = member.Status
		vo.MemberMuteEndTime = member.MuteEndTime
		vo.MemberIsTop = member.IsTop
		vo.MemberIsDisturb = member.IsDisturb
	}

	return vo, nil
}

// JoinCircle 加入圈子。
// 返回值：joined=是否直接加入成功，pending=是否进入待审核。
func (s *circleServiceImpl) JoinCircle(ctx context.Context, userID, circleID uuid.UUID) (bool, bool, error) {
	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		return false, false, err
	}
	if circle.Status != domain.CircleStatusNormal {
		return false, false, errCircleNotAvailable
	}

	member, err := s.memberRepo.JoinCircle(ctx, circleID, userID, circle.JoinType)
	if err != nil {
		return false, false, mapJoinLeaveError(err)
	}

	if member.Status == domain.MemberStatusNormal {
		// 直接加入成功：更新计数缓存 + 发布事件 + 清用户加入列表缓存
		if err := s.incrMemberCountWithRecovery(ctx, circleID); err != nil {
			logger.Log.Error("Failed to update Redis member count: " + err.Error())
		}
		if err := s.publisher.PublishMemberCount(ctx, circleID, 1); err != nil {
			logger.Log.Error("Failed to publish join message: " + err.Error())
		}
		if err := s.joinedCache.InvalidateJoined(ctx, userID); err != nil {
			logger.Log.Error("Failed to delete user joined circles cache: " + err.Error())
		}
		return true, false, nil
	}

	if member.Status == domain.MemberStatusPending {
		return false, true, nil
	}
	return false, false, nil
}

// LeaveCircle 退出圈子。
func (s *circleServiceImpl) LeaveCircle(ctx context.Context, userID, circleID uuid.UUID) error {
	// 检查圈子存在
	if _, err := s.repo.GetByID(ctx, circleID); err != nil {
		return err
	}

	// 检查成员状态
	member, err := s.memberRepo.GetMember(ctx, circleID, userID)
	if err != nil {
		return err
	}

	if err := s.memberRepo.LeaveCircle(ctx, circleID, userID); err != nil {
		return mapJoinLeaveError(err)
	}

	if member.Status == domain.MemberStatusNormal {
		if err := s.decrMemberCountWithRecovery(ctx, circleID); err != nil {
			logger.Log.Error("Failed to update Redis member count: " + err.Error())
		}
		if err := s.publisher.PublishMemberCount(ctx, circleID, -1); err != nil {
			logger.Log.Error("Failed to publish leave message: " + err.Error())
		}
		if err := s.joinedCache.InvalidateJoined(ctx, userID); err != nil {
			logger.Log.Error("Failed to delete user joined circles cache: " + err.Error())
		}
	}
	return nil
}

// SearchCircles 搜索圈子列表。
func (s *circleServiceImpl) SearchCircles(ctx context.Context, keyword string, size int, searchAfter []interface{}) (*CircleSearchResult, error) {
	return s.searcher.Search(ctx, keyword, size, searchAfter)
}

// GetMyCircles 获取我加入的圈子列表。
func (s *circleServiceImpl) GetMyCircles(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error) {
	return s.loadJoinedCircles(ctx, userID, keyword, size, searchAfter)
}

// GetUserCircles 获取任意用户加入的圈子列表（查看「他人」加入的圈子）。
func (s *circleServiceImpl) GetUserCircles(ctx context.Context, targetUserID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error) {
	return s.loadJoinedCircles(ctx, targetUserID, keyword, size, searchAfter)
}

// loadJoinedCircles 加载指定用户加入的圈子列表（GetMyCircles / GetUserCircles 共用）。
//
// 浏览模式（keyword 为空）缓存优先，仅加载前 500 个；搜索模式绕过缓存查全量。
// joinedCache 按 userID key，查「他人」时回填的是对方的 joined 缓存
// （副作用：加速对方自己的 my 查询）。
func (s *circleServiceImpl) loadJoinedCircles(ctx context.Context, userID uuid.UUID, keyword string, size int, searchAfter []interface{}) (*MyCircleSearchResult, error) {
	var circleIDs []uuid.UUID

	if keyword == "" {
		// 浏览模式：缓存优先，仅加载前 500 个
		cached, _ := s.joinedCache.GetJoined(ctx, userID)
		if len(cached) > 0 {
			circleIDs = cached
		} else {
			ids, err := s.memberRepo.GetJoinedCircleIDs(ctx, userID, 500)
			if err != nil {
				return nil, err
			}
			circleIDs = ids
			if err := s.joinedCache.SetJoined(ctx, userID, circleIDs); err != nil {
				logger.Log.Error("Failed to cache joined circle IDs: " + err.Error())
			}
		}
	} else {
		// 搜索模式：绕过缓存，查全量
		ids, err := s.memberRepo.GetJoinedCircleIDs(ctx, userID, 0)
		if err != nil {
			return nil, err
		}
		circleIDs = ids
	}

	if len(circleIDs) == 0 {
		return &MyCircleSearchResult{
			Circles: []MyCircleDoc{}, Total: 0, Size: size, SearchAfter: "",
		}, nil
	}

	return s.searcher.SearchMy(ctx, circleIDs, keyword, size, searchAfter)
}

// GetCirclePosts 获取圈内帖子列表（组装作者/圈子/图片信息）。
func (s *circleServiceImpl) GetCirclePosts(ctx context.Context, circleID uuid.UUID, sortType, size int, searchAfter []interface{}) (*CirclePostResult, error) {
	result, err := s.searcher.SearchCirclePosts(ctx, circleID, sortType, size, searchAfter)
	if err != nil {
		return nil, err
	}

	// 收集 userIDs 和 postIDs
	userIDSet := make(map[uuid.UUID]struct{})
	var postIDs []uuid.UUID
	for _, doc := range result.Posts {
		if postID, err := uuid.Parse(doc.ID); err == nil {
			postIDs = append(postIDs, postID)
		}
		if uid, err := uuid.Parse(doc.UserID); err == nil {
			userIDSet[uid] = struct{}{}
		}
	}
	userIDs := make([]uuid.UUID, 0, len(userIDSet))
	for uid := range userIDSet {
		userIDs = append(userIDs, uid)
	}

	// 圈子信息（所有帖子同一圈子）
	var circleName, circleAvatar string
	if circle, err := s.repo.GetByID(ctx, circleID); err == nil {
		circleName = circle.Name
		circleAvatar = circle.AvatarURL
	}

	// 批量查询用户信息（通过 UserFacade）
	userMap := make(map[uuid.UUID]UserBrief)
	if s.userFacade != nil && len(userIDs) > 0 {
		idStrs := make([]string, 0, len(userIDs))
		for _, id := range userIDs {
			idStrs = append(idStrs, id.String())
		}
		if briefs, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			for idStr, b := range briefs {
				if uid, err := uuid.Parse(idStr); err == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询帖子媒体（通过 PostMediaFetcher）
	mediaMap := make(map[string][]string)
	if s.postFetcher != nil && len(postIDs) > 0 {
		idStrs := make([]string, 0, len(postIDs))
		for _, id := range postIDs {
			idStrs = append(idStrs, id.String())
		}
		if media, err := s.postFetcher.GetMediaByPostIDs(ctx, idStrs); err == nil {
			mediaMap = media
		}
	}

	// 组装帖子列表
	posts := make([]CirclePostListItem, 0, len(result.Posts))
	for _, doc := range result.Posts {
		postID, _ := uuid.Parse(doc.ID)
		uid, _ := uuid.Parse(doc.UserID)
		cid, _ := uuid.Parse(doc.CircleID)

		var authorName, authorAvatar string
		if author, ok := userMap[uid]; ok {
			authorName = author.Username
			authorAvatar = author.AvatarURL
		}

		createTime, _ := time.Parse(time.RFC3339Nano, doc.CreateTime)

		var images []string
		if media, ok := mediaMap[doc.ID]; ok {
			images = media
		}

		posts = append(posts, CirclePostListItem{
			ID: postID, CircleID: cid, UserID: uid, Type: doc.Type,
			Title: doc.Title, Summary: doc.Summary, Content: doc.Content,
			ViewCount: doc.ViewCount, CommentCount: doc.CommentCount,
			LikeCount: doc.LikeCount, CollectCount: doc.CollectCount,
			IsPinned: doc.IsPinned, IsEssence: doc.IsEssence, IsLock: doc.IsLock,
			Status: doc.Status, CreateTime: createTime,
			AuthorName: authorName, AuthorAvatar: authorAvatar,
			CircleName: circleName, CircleAvatar: circleAvatar,
			Images: images,
		})
	}

	return &CirclePostResult{
		Posts: posts, Total: result.Total, Size: result.Size, SearchAfter: result.SearchAfter,
	}, nil
}

// getStatsWithRecovery 获取统计信息，缓存不存在时从 DB 恢复。
//
// 与旧 controller getCircleStatistics / restoreAllCounters 行为一致。
func (s *circleServiceImpl) getStatsWithRecovery(ctx context.Context, circleID uuid.UUID) (memberCount, postCount, hot int) {
	stats, err := s.statsCache.GetStats(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to get circle statistics: " + err.Error())
		return 0, 0, 0
	}
	if stats != nil {
		return stats.MemberCount, stats.PostCount, stats.Hot
	}

	// 缓存未命中，从 DB 恢复
	circle, err := s.repo.GetByID(ctx, circleID)
	if err != nil {
		logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		return 0, 0, 0
	}
	restored := &domain.CircleStatistics{
		MemberCount: circle.MemberCount,
		PostCount:   circle.PostCount,
		Hot:         circle.Hot,
	}
	if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
		logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
	}
	logger.Log.Debug("Restored circle statistics cache",
		zap.String("circle_id", circleID.String()),
		zap.Int("member", circle.MemberCount),
		zap.Int("post", circle.PostCount),
		zap.Int("hot", circle.Hot))
	return circle.MemberCount, circle.PostCount, circle.Hot
}

// incrMemberCountWithRecovery 递增成员计数（缓存不存在时先从 DB 恢复）。
func (s *circleServiceImpl) incrMemberCountWithRecovery(ctx context.Context, circleID uuid.UUID) error {
	exists, err := s.statsCache.StatsExists(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
	}
	if !exists {
		// 从 DB 恢复
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		} else {
			restored := &domain.CircleStatistics{
				MemberCount: circle.MemberCount,
				PostCount:   circle.PostCount,
				Hot:         circle.Hot,
			}
			if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}
	return s.statsCache.IncrMemberCount(ctx, circleID)
}

// decrMemberCountWithRecovery 递减成员计数（缓存不存在时先从 DB 恢复）。
func (s *circleServiceImpl) decrMemberCountWithRecovery(ctx context.Context, circleID uuid.UUID) error {
	exists, err := s.statsCache.StatsExists(ctx, circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
	}
	if !exists {
		circle, err := s.repo.GetByID(ctx, circleID)
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to load circle %s from DB for cache recovery: %s", circleID.String(), err.Error()))
		} else {
			restored := &domain.CircleStatistics{
				MemberCount: circle.MemberCount,
				PostCount:   circle.PostCount,
				Hot:         circle.Hot,
			}
			if err := s.statsCache.SetStats(ctx, circleID, restored); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}
	return s.statsCache.DecrMemberCount(ctx, circleID)
}

// IncrPostCount 发帖后递增圈子帖子计数（Redis 实时 + Redpanda 异步持久化）。
// 供 post 领域通过 CirclePostCountPort 调用。
func (s *circleServiceImpl) IncrPostCount(ctx context.Context, circleID uuid.UUID) error {
	if err := s.statsCache.IncrPostCount(ctx, circleID); err != nil {
		return err
	}
	return s.publisher.PublishPostCount(ctx, circleID)
}
