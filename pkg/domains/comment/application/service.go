// Package application 提供 comment 领域的应用服务层。
//
// 职责：
//   - 发评论/回复（含帖子状态校验、楼层校验、内容清洗）
//   - 获取顶层评论列表（游标分页）
//   - 获取楼层内回复列表（游标分页）
//   - 获取单条评论详情
//   - 组装评论者/被回复人信息（通过 UserFacade）
//   - 点赞状态查询（Redis ZSET 缓存 + DB 回源）
package application

import (
	"context"
	"errors"

	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/utils"

	"github.com/google/uuid"
)

// ===== 跨领域 Facade 依赖 =====

// UserBrief 用户精简视图（与 user.application.UserBrief 字段一致，独立定义避免跨领域 import）。
type UserBrief struct {
	ID        string
	Username  string
	AvatarURL string
}

// UserFacade comment 领域需要的 user 查询接口。
type UserFacade interface {
	// GetBriefs 批量获取用户精简视图。未找到的用户不会出现在 map 里。
	GetBriefs(ctx context.Context, userIDs []string) (map[string]UserBrief, error)
	// GetBrief 获取单个用户精简视图。未找到返回 nil, nil。
	GetBrief(ctx context.Context, userID string) (*UserBrief, error)
}

// PostInfo comment 领域需要的帖子信息（发评论时校验用）。
type PostInfo struct {
	ID     uuid.UUID
	Status int16 // 帖子状态（PostStatusPublished=1 表示可评论）
	IsLock int16 // 是否锁定（1=锁定）
}

// PostLookup comment 领域需要的帖子查询端口。
//
// 用于发评论时校验帖子存在性/状态/锁定，以及恢复帖子统计缓存。
type PostLookup interface {
	// GetPost 返回帖子信息。未找到返回 nil, nil。
	GetPost(ctx context.Context, postID uuid.UUID) (*PostInfo, error)
	// RestoreStatsAndIncrCommentCount 恢复帖子统计缓存（如果不存在），
	// 然后递增帖子评论计数（Redis Hash + Redpanda 异步持久化）。
	RestoreStatsAndIncrCommentCount(ctx context.Context, postID uuid.UUID) error
}

// ===== DTO =====

// CommentVO 评论 VO（包含评论信息 + 评论者信息 + 被回复人信息 + 当前用户点赞状态）。
type CommentVO struct {
	ID         uuid.UUID  `json:"id"`
	PostID     uuid.UUID  `json:"post_id"`
	UserID     uuid.UUID  `json:"user_id"`
	RootID     *uuid.UUID `json:"root_id,omitempty"`
	ReplyToID  *uuid.UUID `json:"reply_to_id,omitempty"`
	Content    string     `json:"content"`
	LikeCount  int        `json:"like_count"`
	ReplyCount int        `json:"reply_count"`
	Status     int16      `json:"status"`
	CreateTime string     `json:"create_time"`

	// 扩展数据（JSON格式，包含图片URL数组等）
	ExtraData []byte `json:"extra_data,omitempty"`

	// 评论者信息
	AuthorName   string `json:"author_name"`
	AuthorAvatar string `json:"author_avatar"`

	// 被回复人信息（仅回复时有值）
	ReplyToUserID *uuid.UUID `json:"reply_to_user_id,omitempty"`
	ReplyToName   string     `json:"reply_to_name,omitempty"`

	// 用户交互状态
	Liked bool `json:"liked"` // 当前用户是否点赞了该评论
}

// CommentListResult 评论列表结果（游标分页）。
type CommentListResult struct {
	Items      []CommentVO `json:"items"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor"`
}

// CreateCommentInput 发评论/回复入参。
type CreateCommentInput struct {
	PostID    uuid.UUID
	Content   string
	ExtraData []byte // json.RawMessage
	RootID    *uuid.UUID
	ReplyToID *uuid.UUID
}

// ===== Service 接口 =====

// CommentService 是 comment 领域的应用服务接口。
type CommentService interface {
	// CreateComment 发评论（支持顶层评论和回复）。
	CreateComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput) (uuid.UUID, error)
	// GetRootComments 获取帖子的顶层评论列表（游标分页）。
	// sort: 0=按点赞倒序(默认), 1=按时间倒序。
	GetRootComments(ctx context.Context, userID, postID uuid.UUID, sort int, cursor string) (*CommentListResult, error)
	// GetReplies 获取某条评论的子回复列表（游标分页）。
	// sort: 0=按时间倒序(默认), 1=按点赞倒序。
	GetReplies(ctx context.Context, userID, rootID uuid.UUID, limit, sort int, cursor string) (*CommentListResult, error)
	// GetCommentDetail 获取单条评论详情。
	GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error)

	// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
	// 返回评论所属帖子ID。未找到返回 nil, nil。
	GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error)
	// RestoreCommentStats 恢复评论统计缓存（如果不存在）。
	// 供 like 领域点赞前确保 Redis stats Hash 存在。
	RestoreCommentStats(ctx context.Context, commentID uuid.UUID) error

	// SetUserFacade 注入 user Facade（组装评论者/被回复人信息用）。
	SetUserFacade(f UserFacade)
	// SetPostLookup 注入帖子查询端口（发评论校验 + 帖子评论计数用）。
	SetPostLookup(p PostLookup)
}

type commentServiceImpl struct {
	repo       domain.CommentRepository
	statsCache domain.CommentStatsCache
	likeCache  domain.CommentLikeCache
	publisher  domain.CommentEventPublisher
	userFacade UserFacade
	postLookup PostLookup
}

// NewCommentService 构造 CommentService。
//
// userFacade / postLookup 是跨领域依赖，通过 setter 注入（composition 层负责把它们连起来）。
func NewCommentService(
	repo domain.CommentRepository,
	statsCache domain.CommentStatsCache,
	likeCache domain.CommentLikeCache,
	publisher domain.CommentEventPublisher,
) CommentService {
	return &commentServiceImpl{
		repo:       repo,
		statsCache: statsCache,
		likeCache:  likeCache,
		publisher:  publisher,
	}
}

// Setter 方法供 composition 注入跨领域依赖。
func (s *commentServiceImpl) SetUserFacade(f UserFacade) { s.userFacade = f }
func (s *commentServiceImpl) SetPostLookup(p PostLookup) { s.postLookup = p }

// CreateComment 发评论（支持顶层评论和回复）。
//
// 与旧 controller.CreateComment 行为一致：
//  1. 校验帖子存在 + 状态(已发布) + 未锁定；
//  2. 如果是回复，校验 root_id 属于同一帖子，校验 reply_to_id 在同一楼层，获取被回复用户ID；
//  3. 清洗 content（SanitizeForPg，剔除 NULL 字节等非法 UTF8）；
//  4. 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）；
//  5. 实时递增帖子评论计数（Redis Hash），并发布 Redpanda 异步持久化事件。
func (s *commentServiceImpl) CreateComment(ctx context.Context, userID uuid.UUID, input CreateCommentInput) (uuid.UUID, error) {
	// 1. 校验帖子
	if s.postLookup == nil {
		return uuid.Nil, errors.New("post lookup is not configured")
	}
	post, err := s.postLookup.GetPost(ctx, input.PostID)
	if err != nil {
		return uuid.Nil, err
	}
	if post == nil {
		return uuid.Nil, domain.ErrPostNotFound
	}
	// 帖子状态必须为"已发布"才能评论（PostStatusPublished=1）
	if post.Status != 1 {
		return uuid.Nil, domain.ErrPostNotCommentable
	}
	if post.IsLock == 1 {
		return uuid.Nil, domain.ErrPostLocked
	}

	// 2. 如果是回复，校验 root_id 和 reply_to_id，并获取被回复用户ID
	var replyToUserID *uuid.UUID
	if input.RootID != nil {
		rootID := *input.RootID
		// 校验根评论存在且属于同一帖子
		rootComment, err := s.repo.GetByID(ctx, rootID)
		if err != nil {
			if errors.Is(err, domain.ErrCommentNotFound) {
				return uuid.Nil, domain.ErrRootCommentNotFound
			}
			return uuid.Nil, err
		}
		if rootComment.PostID != input.PostID {
			return uuid.Nil, domain.ErrRootCommentMismatch
		}

		// 如果指定了 reply_to_id，校验被回复的评论存在并获取被回复用户ID
		if input.ReplyToID != nil {
			replyToComment, err := s.repo.GetByID(ctx, *input.ReplyToID)
			if err != nil {
				if errors.Is(err, domain.ErrCommentNotFound) {
					return uuid.Nil, domain.ErrReplyTargetNotFound
				}
				return uuid.Nil, err
			}
			// 被回复的评论必须属于同一个根评论下
			// (要么是被回复评论本身就是根评论，要么其 root_id 等于当前根评论ID)
			valid := replyToComment.ID == rootID ||
				(replyToComment.RootID != nil && *replyToComment.RootID == rootID)
			if !valid {
				return uuid.Nil, domain.ErrReplyTargetNotInThread
			}
			// 获取被回复用户ID
			uid := replyToComment.UserID
			replyToUserID = &uid
		}
	}

	// 3. 清洗 PostgreSQL text 字段不接受的字符（NULL 字节 U+0000 及其它无效
	// UTF-8 字节序列），避免写入时报 "invalid byte sequence for encoding UTF8"。
	content := utils.SanitizeForPg(input.Content)
	if content == "" {
		return uuid.Nil, domain.ErrEmptyContent
	}

	// 4. 构建评论数据并创建
	comment := &domain.Comment{
		PostID:        input.PostID,
		UserID:        userID,
		RootID:        input.RootID,
		ReplyToID:     input.ReplyToID,
		ReplyToUserID: replyToUserID,
		Content:       content,
		ExtraData:     input.ExtraData,
		Status:        domain.CommentStatusNormal,
		Deleted:       0,
	}
	if err := s.repo.Create(ctx, comment); err != nil {
		return uuid.Nil, err
	}

	// 5. 实时递增帖子评论计数（Redis Hash + 恢复缓存），并发布 Redpanda 异步持久化事件
	if err := s.postLookup.RestoreStatsAndIncrCommentCount(ctx, input.PostID); err != nil {
		logger.Log.Error("Failed to increment post comment count: " + err.Error())
	}

	return comment.ID, nil
}

// GetRootComments 获取帖子的顶层评论列表（游标分页）。
func (s *commentServiceImpl) GetRootComments(ctx context.Context, userID, postID uuid.UUID, sort int, cursor string) (*CommentListResult, error) {
	comments, nextCursor, hasMore, err := s.repo.GetRootCommentsByCursor(ctx, postID, 20, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// GetReplies 获取某条评论的子回复列表（游标分页）。
func (s *commentServiceImpl) GetReplies(ctx context.Context, userID, rootID uuid.UUID, limit, sort int, cursor string) (*CommentListResult, error) {
	// 校验根评论存在且是顶层评论
	rootComment, err := s.repo.GetByID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if rootComment.RootID != nil {
		return nil, errNotRootComment
	}

	if limit <= 0 {
		limit = 10
	}
	comments, nextCursor, hasMore, err := s.repo.GetRepliesByCursor(ctx, rootID, limit, sort, cursor)
	if err != nil {
		return nil, err
	}
	vos := s.buildCommentVOs(ctx, userID, comments)
	return &CommentListResult{
		Items: vos, HasMore: hasMore, NextCursor: nextCursor,
	}, nil
}

// GetCommentDetail 获取单条评论详情。
func (s *commentServiceImpl) GetCommentDetail(ctx context.Context, userID, commentID uuid.UUID) (*CommentVO, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return nil, err
	}

	vo := &CommentVO{
		ID:         comment.ID,
		PostID:     comment.PostID,
		UserID:     comment.UserID,
		RootID:     comment.RootID,
		ReplyToID:  comment.ReplyToID,
		Content:    comment.Content,
		ExtraData:  comment.ExtraData,
		LikeCount:  comment.LikeCount,
		ReplyCount: comment.ReplyCount,
		Status:     comment.Status,
		CreateTime: comment.CreateTime.Format("2006-01-02 15:04:05"),
	}

	// 获取当前用户点赞状态（缓存优先，miss 回源 DB + 回填）
	vo.Liked = s.checkLiked(ctx, userID, commentID)

	// 填充评论者信息
	if s.userFacade != nil {
		if author, err := s.userFacade.GetBrief(ctx, comment.UserID.String()); err == nil && author != nil {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}
	}

	// 填充被回复人信息
	if comment.ReplyToUserID != nil {
		vo.ReplyToUserID = comment.ReplyToUserID
		if s.userFacade != nil {
			if replyUser, err := s.userFacade.GetBrief(ctx, comment.ReplyToUserID.String()); err == nil && replyUser != nil {
				vo.ReplyToName = replyUser.Username
			}
		}
	}

	return vo, nil
}

// GetCommentMeta 获取评论元信息（供 like 领域校验用）。
// 返回评论所属帖子ID（冗余字段，供 like 事件消费者更新 comment.post_id）。
// 未找到返回 nil, nil。
func (s *commentServiceImpl) GetCommentMeta(ctx context.Context, commentID uuid.UUID) (*uuid.UUID, error) {
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			return nil, nil
		}
		return nil, err
	}
	postID := comment.PostID
	return &postID, nil
}

// RestoreCommentStats 恢复评论统计缓存（如果不存在）。
// 供 like 领域点赞前确保 Redis stats Hash 存在，避免 Lua 脚本读到空 stats。
//
// 与旧 controller.restoreCommentStatsIfNeed 行为一致：
// 检查缓存是否存在 → 不存在则从 DB 读 like_count 写入 Redis。
func (s *commentServiceImpl) RestoreCommentStats(ctx context.Context, commentID uuid.UUID) error {
	exists, err := s.statsCache.Exists(ctx, commentID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return err
	}
	return s.statsCache.Set(ctx, commentID, comment.LikeCount)
}

// buildCommentVOs 批量构建 CommentVO（包含评论者信息和被回复人信息）。
//
// 与旧 controller.buildCommentVOs 行为一致：
//  1. 收集所有需要查询用户信息的用户ID（评论者 + 被回复人）；
//  2. 批量查询所有用户信息；
//  3. 批量查询当前用户点赞状态（Redis 优先，miss 回源 DB + 回填）；
//  4. 组装 VO。
func (s *commentServiceImpl) buildCommentVOs(ctx context.Context, userID uuid.UUID, comments []domain.Comment) []CommentVO {
	// 收集所有需要查询用户信息的用户ID
	userIDSet := make(map[uuid.UUID]struct{})
	for _, cm := range comments {
		userIDSet[cm.UserID] = struct{}{}
		if cm.ReplyToUserID != nil {
			userIDSet[*cm.ReplyToUserID] = struct{}{}
		}
	}

	// 批量查询所有用户信息
	userMap := make(map[uuid.UUID]UserBrief, len(userIDSet))
	if s.userFacade != nil && len(userIDSet) > 0 {
		idStrs := make([]string, 0, len(userIDSet))
		for id := range userIDSet {
			idStrs = append(idStrs, id.String())
		}
		if briefs, err := s.userFacade.GetBriefs(ctx, idStrs); err == nil {
			for idStr, b := range briefs {
				if uid, parseErr := uuid.Parse(idStr); parseErr == nil {
					userMap[uid] = UserBrief{ID: b.ID, Username: b.Username, AvatarURL: b.AvatarURL}
				}
			}
		}
	}

	// 批量查询当前用户点赞状态
	likedMap := s.batchLikedStatus(ctx, userID, comments)

	// 组装
	vos := make([]CommentVO, 0, len(comments))
	for _, cm := range comments {
		vo := CommentVO{
			ID:         cm.ID,
			PostID:     cm.PostID,
			UserID:     cm.UserID,
			RootID:     cm.RootID,
			ReplyToID:  cm.ReplyToID,
			Content:    cm.Content,
			ExtraData:  cm.ExtraData,
			LikeCount:  cm.LikeCount,
			ReplyCount: cm.ReplyCount,
			Status:     cm.Status,
			CreateTime: cm.CreateTime.Format("2006-01-02 15:04:05"),
			Liked:      likedMap[cm.ID],
		}

		// 填充评论者信息
		if author, exists := userMap[cm.UserID]; exists {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}

		// 填充被回复人信息
		if cm.ReplyToUserID != nil {
			vo.ReplyToUserID = cm.ReplyToUserID
			if replyUser, exists := userMap[*cm.ReplyToUserID]; exists {
				vo.ReplyToName = replyUser.Username
			}
		}

		vos = append(vos, vo)
	}

	return vos
}

// batchLikedStatus 批量获取评论点赞状态（先查 Redis ZSET，miss 时回源 DB）。
//
// 与旧 controller.getCommentLikedStatus 行为一致。
func (s *commentServiceImpl) batchLikedStatus(ctx context.Context, userID uuid.UUID, comments []domain.Comment) map[uuid.UUID]bool {
	// 匿名访问（userID == uuid.Nil）直接返回空 map，避免对幽灵 key
	// "user:like:comments:00000000-..." 发起无意义的 Redis ZMScore + DB 回源。
	// 与 checkLiked 的 uuid.Nil 短路行为保持一致。
	if userID == uuid.Nil {
		return make(map[uuid.UUID]bool, len(comments))
	}

	commentIDs := make([]uuid.UUID, len(comments))
	for i, cm := range comments {
		commentIDs[i] = cm.ID
	}

	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool)
	}

	// 1. Batch check from Redis ZSET
	likedMap, err := s.likeCache.BatchCheck(ctx, userID, commentIDs)
	if err != nil {
		logger.Log.Error("Failed to batch check comment liked from Redis: " + err.Error())
		likedMap = make(map[uuid.UUID]bool)
	}

	// 2. Find cache misses
	var missIDs []uuid.UUID
	for _, id := range commentIDs {
		if !likedMap[id] {
			missIDs = append(missIDs, id)
		}
	}

	// 3. Fallback to DB for cache misses
	if len(missIDs) > 0 {
		dbLiked, err := s.repo.BatchCheckLiked(ctx, userID, missIDs)
		if err != nil {
			logger.Log.Error("Failed to batch check comment liked from DB: " + err.Error())
		} else {
			// Merge DB results into likedMap
			for id, liked := range dbLiked {
				likedMap[id] = liked
			}
			// Backfill ZSET for DB-confirmed likes
			var backfillIDs []uuid.UUID
			for _, id := range missIDs {
				if dbLiked[id] {
					backfillIDs = append(backfillIDs, id)
				}
			}
			if len(backfillIDs) > 0 {
				if err := s.likeCache.Backfill(ctx, userID, backfillIDs); err != nil {
					logger.Log.Error("Failed to backfill comment likes to Redis: " + err.Error())
				}
			}
		}
	}

	return likedMap
}

// checkLiked 检查单条评论的点赞状态（缓存优先，miss 回源 DB + 回填）。
//
// 与旧 controller.GetCommentDetail 中的点赞状态查询逻辑一致。
func (s *commentServiceImpl) checkLiked(ctx context.Context, userID, commentID uuid.UUID) bool {
	if userID == uuid.Nil {
		return false
	}
	likedMap, err := s.likeCache.BatchCheck(ctx, userID, []uuid.UUID{commentID})
	if err != nil {
		// 缓存故障：直接回源 DB
		isLiked, dbErr := s.repo.IsLiked(ctx, userID, commentID)
		if dbErr != nil {
			return false
		}
		return isLiked
	}

	if likedMap[commentID] {
		return true
	}
	if !likedMap[commentID] {
		// 缓存明确表示未命中（score==0），回源 DB
		isLiked, dbErr := s.repo.IsLiked(ctx, userID, commentID)
		if dbErr != nil {
			return false
		}
		if isLiked {
			_ = s.likeCache.Backfill(ctx, userID, []uuid.UUID{commentID})
		}
		return isLiked
	}
	return false
}
