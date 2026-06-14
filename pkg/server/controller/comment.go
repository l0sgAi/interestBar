package controller

import (
	"encoding/json"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	redpanda "interestBar/pkg/server/storage/redpanda"
	"interestBar/pkg/server/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CommentController 处理评论相关操作
type CommentController struct{}

func NewCommentController() *CommentController {
	return &CommentController{}
}

// CreateCommentRequest 发评论/回复的请求结构
type CreateCommentRequest struct {
	PostID    uuid.UUID       `json:"post_id" binding:"required"`
	Content   string          `json:"content" binding:"required,min=1,max=10000"`
	ExtraData json.RawMessage `json:"extra_data" binding:"omitempty"`  // 扩展数据（JSON格式，如图片URL数组等）
	RootID    *uuid.UUID      `json:"root_id" binding:"omitempty"`     // 根评论ID，nil或不传=顶层评论
	ReplyToID *uuid.UUID      `json:"reply_to_id" binding:"omitempty"` // 被回复的评论ID
}

// CreateComment 发评论（支持顶层评论和回复）
// POST /comment/create
func (ctrl *CommentController) CreateComment(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req CreateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 1. 检查帖子是否存在
	post, err := model.GetPostByID(pgsql.DB, req.PostID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Post not found")
			return
		}
		response.InternalError(c, "Failed to check post")
		return
	}

	// 检查帖子状态
	if post.Status != model.PostStatusPublished {
		response.Forbidden(c, "Cannot comment on this post")
		return
	}

	// 检查帖子是否被锁定
	if post.IsLock == 1 {
		response.Forbidden(c, "This post is locked, comments are not allowed")
		return
	}

	// 2. 如果是回复，校验 root_id 和 reply_to_id，并获取被回复用户ID
	var replyToUserID *uuid.UUID
	if req.RootID != nil {
		rootID := *req.RootID
		// 校验根评论存在且属于同一帖子
		rootComment, err := model.GetCommentByID(pgsql.DB, rootID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				response.NotFound(c, "Root comment not found")
				return
			}
			response.InternalError(c, "Failed to check root comment")
			return
		}
		if rootComment.PostID != req.PostID {
			response.BadRequest(c, "Root comment does not belong to this post")
			return
		}

		// 如果指定了 reply_to_id，校验被回复的评论存在并获取被回复用户ID
		if req.ReplyToID != nil {
			replyToComment, err := model.GetCommentByID(pgsql.DB, *req.ReplyToID)
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					response.NotFound(c, "Reply target comment not found")
					return
				}
				response.InternalError(c, "Failed to check reply target")
				return
			}
			// 被回复的评论必须属于同一个根评论下
			// (要么是被回复评论本身就是根评论，要么其 root_id 等于当前根评论ID)
			valid := replyToComment.ID == rootID ||
				(replyToComment.RootID != nil && *replyToComment.RootID == rootID)
			if !valid {
				response.BadRequest(c, "Reply target does not belong to the same thread")
				return
			}
			// 获取被回复用户ID
			uid := replyToComment.UserID
			replyToUserID = &uid
		}
	}

	// 3. 清洗 PostgreSQL text 字段不接受的字符（NULL 字节 U+0000 及其它无效
	// UTF-8 字节序列），避免写入时报 "invalid byte sequence for encoding UTF8"
	// (SQLSTATE 22021)。这些字节常来自富文本/Markdown 粘贴残留。
	content := utils.SanitizeForPg(req.Content)
	if content == "" {
		response.BadRequest(c, "Comment content is empty")
		return
	}

	// 4. 构建评论数据
	comment := model.Comment{
		PostID:        req.PostID,
		UserID:        userID,
		RootID:        req.RootID,
		ReplyToID:     req.ReplyToID,
		ReplyToUserID: replyToUserID,
		Content:       content,
		ExtraData:     req.ExtraData,
		Status:        model.CommentStatusNormal,
		Deleted:       0,
	}

	// 5. 创建评论（事务：插入评论 + 更新根评论回复计数）
	if err := model.CreateComment(pgsql.DB, &comment); err != nil {
		response.InternalError(c, "Failed to create comment")
		return
	}

	// 6. 实时更新帖子评论计数（Redis Hash）
	if err := restorePostStatsIfNeed(req.PostID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}
	if err := redispkg.IncrementPostCommentCount(req.PostID); err != nil {
		logger.Log.Error("Failed to increment post comment count in Redis: " + err.Error())
	}

	// 7. 发送Kafka消息用于持久化到数据库
	if err := redpanda.PublishPostCommentCount(req.PostID, 1); err != nil {
		logger.Log.Error("Failed to publish post comment count message: " + err.Error())
	}

	response.SuccessWithMessage(c, "评论成功", comment.ID)
}

// CommentVO 评论VO（包含评论信息 + 评论者信息）
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
	ExtraData json.RawMessage `json:"extra_data,omitempty"`

	// 评论者信息
	AuthorName   string `json:"author_name"`
	AuthorAvatar string `json:"author_avatar"`

	// 被回复人信息（仅回复时有值）
	ReplyToUserID *uuid.UUID `json:"reply_to_user_id,omitempty"`
	ReplyToName   string     `json:"reply_to_name,omitempty"`

	// 用户交互状态
	Liked bool `json:"liked"` // 当前用户是否点赞了该评论
}

// GetCommentsRequest 获取顶层评论列表的请求结构
type GetCommentsRequest struct {
	PostID string `form:"post_id" binding:"required,uuid"`
	Sort   int    `form:"sort" binding:"omitempty,oneof=0 1"` // 0=点赞倒序(默认), 1=时间倒序
	Cursor string `form:"cursor"`                             // 游标，首页不传
}

// GetComments 获取帖子的顶层评论列表（游标分页）
// GET /comment/list
func (ctrl *CommentController) GetComments(c *gin.Context) {
	var req GetCommentsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	postID, err := uuid.Parse(req.PostID)
	if err != nil {
		response.BadRequest(c, "Invalid post_id")
		return
	}

	comments, nextCursor, hasMore, err := model.GetRootCommentsByCursor(pgsql.DB, postID, 20, req.Sort, req.Cursor)
	if err != nil {
		response.InternalError(c, "Failed to get comments")
		return
	}

	// 获取当前用户点赞状态
	userID, _ := utils.GetUserIDFromRequest(c)
	var likedMap map[uuid.UUID]bool
	if userID != uuid.Nil && len(comments) > 0 {
		likedMap = getCommentLikedStatus(userID, comments)
	}

	vos := buildCommentVOs(comments, likedMap)

	response.Success(c, map[string]interface{}{
		"items":       vos,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

// GetRepliesRequest 获取回复列表的请求结构
type GetRepliesRequest struct {
	RootID string `form:"root_id" binding:"required,uuid"`
	Sort   int    `form:"sort" binding:"omitempty,oneof=0 1"` // 0=时间倒序(默认), 1=点赞倒序
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=50"` // 每页条数，默认10
}

// GetReplies 获取某条评论的子回复列表（游标分页）
// GET /comment/replies
func (ctrl *CommentController) GetReplies(c *gin.Context) {
	var req GetRepliesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	rootID, err := uuid.Parse(req.RootID)
	if err != nil {
		response.BadRequest(c, "Invalid root_id")
		return
	}

	// 校验根评论存在且是顶层评论
	rootComment, err := model.GetCommentByID(pgsql.DB, rootID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Root comment not found")
			return
		}
		response.InternalError(c, "Failed to check root comment")
		return
	}
	if rootComment.RootID != nil {
		response.BadRequest(c, "Not a root comment")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	comments, nextCursor, hasMore, err := model.GetRepliesByCursor(pgsql.DB, rootID, limit, req.Sort, req.Cursor)
	if err != nil {
		response.InternalError(c, "Failed to get replies")
		return
	}

	// 获取当前用户点赞状态
	userID, _ := utils.GetUserIDFromRequest(c)
	var likedMap map[uuid.UUID]bool
	if userID != uuid.Nil && len(comments) > 0 {
		likedMap = getCommentLikedStatus(userID, comments)
	}

	vos := buildCommentVOs(comments, likedMap)

	response.Success(c, map[string]interface{}{
		"items":       vos,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

// buildCommentVOs 批量构建 CommentVO（包含评论者信息和被回复人信息）
func buildCommentVOs(comments []model.Comment, likedMap map[uuid.UUID]bool) []CommentVO {
	// 收集所有需要查询用户信息的用户ID
	userIDSet := make(map[uuid.UUID]struct{})
	for _, cm := range comments {
		userIDSet[cm.UserID] = struct{}{}
		// 收集被回复用户ID（直接从模型中的 ReplyToUserID 获取）
		if cm.ReplyToUserID != nil {
			userIDSet[*cm.ReplyToUserID] = struct{}{}
		}
	}

	// 批量查询所有用户信息
	userIDs := make([]uuid.UUID, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}
	userMap, _ := model.GetUsersByIDs(pgsql.DB, userIDs)

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

		// 填充被回复人信息（直接从模型中的 ReplyToUserID 获取）
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

// GetCommentDetail 获取单条评论详情
// GET /comment/detail/:id
func (ctrl *CommentController) GetCommentDetail(c *gin.Context) {
	commentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid comment id")
		return
	}

	comment, err := model.GetCommentByID(pgsql.DB, commentID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Comment not found")
			return
		}
		response.InternalError(c, "Failed to get comment")
		return
	}

	vo := CommentVO{
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

	// 获取当前用户点赞状态
	curUserID, _ := utils.GetUserIDFromRequest(c)
	if curUserID != uuid.Nil {
		likedMap, cacheErr := redispkg.BatchCheckCommentLiked(curUserID, []uuid.UUID{commentID})
		if cacheErr == nil && likedMap[commentID] {
			vo.Liked = true
		} else if cacheErr == nil && !likedMap[commentID] {
			isLiked, dbErr := model.IsCommentLiked(pgsql.DB, curUserID, commentID)
			if dbErr == nil {
				vo.Liked = isLiked
				if isLiked {
					redispkg.BackfillCommentLikes(curUserID, []uuid.UUID{commentID})
				}
			}
		}
	}

	// 填充评论者信息
	if author, err := model.GetUserByID(pgsql.DB, comment.UserID); err == nil {
		vo.AuthorName = author.Username
		vo.AuthorAvatar = author.AvatarURL
	}

	// 填充被回复人信息（直接从模型中的 ReplyToUserID 获取）
	if comment.ReplyToUserID != nil {
		vo.ReplyToUserID = comment.ReplyToUserID
		if replyUser, err := model.GetUserByID(pgsql.DB, *comment.ReplyToUserID); err == nil {
			vo.ReplyToName = replyUser.Username
		}
	}

	response.Success(c, vo)
}

// getCommentLikedStatus 批量获取评论点赞状态（先查Redis ZSET，miss时回源DB）
func getCommentLikedStatus(userID uuid.UUID, comments []model.Comment) map[uuid.UUID]bool {
	commentIDs := make([]uuid.UUID, len(comments))
	for i, cm := range comments {
		commentIDs[i] = cm.ID
	}

	// 1. Batch check from Redis ZSET
	likedMap, err := redispkg.BatchCheckCommentLiked(userID, commentIDs)
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
		dbLiked, err := model.BatchCheckCommentLiked(pgsql.DB, userID, missIDs)
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
				if err := redispkg.BackfillCommentLikes(userID, backfillIDs); err != nil {
					logger.Log.Error("Failed to backfill comment likes to Redis: " + err.Error())
				}
			}
		}
	}

	return likedMap
}

// restorePostStatsIfNeed 恢复帖子统计信息到Redis缓存（如果缓存不存在）
func restorePostStatsIfNeed(postID uuid.UUID) error {
	exists, err := redispkg.PostStatisticsExists(postID)
	if err != nil || exists {
		return err
	}

	post, err := model.GetPostByID(pgsql.DB, postID)
	if err != nil {
		return err
	}

	return redispkg.UpdatePostStatistics(postID, &redispkg.PostStatistics{
		ViewCount:    post.ViewCount,
		CommentCount: post.CommentCount,
		LikeCount:    post.LikeCount,
		CollectCount: post.CollectCount,
	})
}
