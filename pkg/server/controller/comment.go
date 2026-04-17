package controller

import (
	"fmt"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	redpanda "interestBar/pkg/server/storage/redpanda"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CommentController 处理评论相关操作
type CommentController struct{}

func NewCommentController() *CommentController {
	return &CommentController{}
}

// CreateCommentRequest 发评论/回复的请求结构
type CreateCommentRequest struct {
	PostID    int64  `json:"post_id" binding:"required,min=1"`
	Content   string `json:"content" binding:"required,min=1,max=10000"`
	RootID    int64  `json:"root_id" binding:"omitempty,min=1"`     // 根评论ID，0或不传=顶层评论
	ReplyToID int64  `json:"reply_to_id" binding:"omitempty,min=1"` // 被回复的评论ID
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
	var replyToUserID int64 = 0
	if req.RootID > 0 {
		// 校验根评论存在且属于同一帖子
		rootComment, err := model.GetCommentByID(pgsql.DB, req.RootID)
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
		if req.ReplyToID > 0 {
			replyToComment, err := model.GetCommentByID(pgsql.DB, req.ReplyToID)
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					response.NotFound(c, "Reply target comment not found")
					return
				}
				response.InternalError(c, "Failed to check reply target")
				return
			}
			// 被回复的评论必须属于同一个根评论下
			if replyToComment.RootID != req.RootID && replyToComment.ID != req.RootID {
				response.BadRequest(c, "Reply target does not belong to the same thread")
				return
			}
			// 获取被回复用户ID
			replyToUserID = replyToComment.UserID
		}
	}

	// 3. 构建评论数据
	comment := model.Comment{
		PostID:        req.PostID,
		UserID:        int64(userID),
		RootID:        req.RootID,
		ReplyToID:     req.ReplyToID,
		ReplyToUserID: replyToUserID,
		Content:       req.Content,
		Status:        model.CommentStatusNormal,
		Deleted:       0,
	}

	// 4. 创建评论（事务：插入评论 + 更新根评论回复计数）
	if err := model.CreateComment(pgsql.DB, &comment); err != nil {
		response.InternalError(c, "Failed to create comment")
		return
	}

	// 5. 实时更新帖子评论计数（Redis Hash）
	if err := restorePostStatsIfNeed(req.PostID); err != nil {
		logger.Log.Error("Failed to restore post stats cache: " + err.Error())
	}
	if err := redispkg.IncrementPostCommentCount(req.PostID); err != nil {
		logger.Log.Error("Failed to increment post comment count in Redis: " + err.Error())
	}

	// 6. 发送Kafka消息用于持久化到数据库
	if err := redpanda.PublishPostCommentCount(req.PostID, 1); err != nil {
		logger.Log.Error("Failed to publish post comment count message: " + err.Error())
	}

	response.SuccessWithMessage(c, "评论成功", comment.ID)
}

// CommentVO 评论VO（包含评论信息 + 评论者信息）
type CommentVO struct {
	ID         int64  `json:"id"`
	PostID     int64  `json:"post_id"`
	UserID     int64  `json:"user_id"`
	RootID     int64  `json:"root_id"`
	ReplyToID  int64  `json:"reply_to_id"`
	Content    string `json:"content"`
	LikeCount  int    `json:"like_count"`
	ReplyCount int    `json:"reply_count"`
	Status     int16  `json:"status"`
	CreateTime string `json:"create_time"`

	// 评论者信息
	AuthorName   string `json:"author_name"`
	AuthorAvatar string `json:"author_avatar"`

	// 被回复人信息（仅回复时有值）
	ReplyToUserID int64  `json:"reply_to_user_id,omitempty"`
	ReplyToName   string `json:"reply_to_name,omitempty"`
}

// GetCommentsRequest 获取顶层评论列表的请求结构
type GetCommentsRequest struct {
	PostID int64  `form:"post_id" binding:"required,min=1"`
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

	comments, nextCursor, hasMore, err := model.GetRootCommentsByCursor(pgsql.DB, req.PostID, 20, req.Sort, req.Cursor)
	if err != nil {
		response.InternalError(c, "Failed to get comments")
		return
	}

	vos := buildCommentVOs(comments)

	response.Success(c, map[string]interface{}{
		"items":       vos,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

// GetRepliesRequest 获取回复列表的请求结构
type GetRepliesRequest struct {
	RootID int64  `form:"root_id" binding:"required,min=1"`
	Sort   int    `form:"sort" binding:"omitempty,oneof=0 1"` // 0=时间倒序(默认), 1=点赞倒序
	Cursor string `form:"cursor"`
}

// GetReplies 获取某条评论的子回复列表（游标分页）
// GET /comment/replies
func (ctrl *CommentController) GetReplies(c *gin.Context) {
	var req GetRepliesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 校验根评论存在且是顶层评论
	rootComment, err := model.GetCommentByID(pgsql.DB, req.RootID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Root comment not found")
			return
		}
		response.InternalError(c, "Failed to check root comment")
		return
	}
	if rootComment.RootID != 0 {
		response.BadRequest(c, "Not a root comment")
		return
	}

	comments, nextCursor, hasMore, err := model.GetRepliesByCursor(pgsql.DB, req.RootID, 5, req.Sort, req.Cursor)
	if err != nil {
		response.InternalError(c, "Failed to get replies")
		return
	}

	vos := buildCommentVOs(comments)

	response.Success(c, map[string]interface{}{
		"items":       vos,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

// buildCommentVOs 批量构建 CommentVO（包含评论者信息和被回复人信息）
func buildCommentVOs(comments []model.Comment) []CommentVO {
	// 收集所有需要查询用户信息的用户ID
	userIDSet := make(map[int64]struct{})
	for _, cm := range comments {
		userIDSet[cm.UserID] = struct{}{}
		// 收集被回复用户ID（直接从模型中的 ReplyToUserID 获取）
		if cm.ReplyToUserID > 0 {
			userIDSet[cm.ReplyToUserID] = struct{}{}
		}
	}

	// 批量查询所有用户信息
	userIDs := make([]int64, 0, len(userIDSet))
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
			LikeCount:  cm.LikeCount,
			ReplyCount: cm.ReplyCount,
			Status:     cm.Status,
			CreateTime: cm.CreateTime.Format("2006-01-02 15:04:05"),
		}

		// 填充评论者信息
		if author, exists := userMap[cm.UserID]; exists {
			vo.AuthorName = author.Username
			vo.AuthorAvatar = author.AvatarURL
		}

		// 填充被回复人信息（直接从模型中的 ReplyToUserID 获取）
		if cm.ReplyToUserID > 0 {
			vo.ReplyToUserID = cm.ReplyToUserID
			if replyUser, exists := userMap[cm.ReplyToUserID]; exists {
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
	commentIDStr := c.Param("id")
	var commentID int64
	if _, err := fmt.Sscanf(commentIDStr, "%d", &commentID); err != nil || commentID <= 0 {
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
		LikeCount:  comment.LikeCount,
		ReplyCount: comment.ReplyCount,
		Status:     comment.Status,
		CreateTime: comment.CreateTime.Format("2006-01-02 15:04:05"),
	}

	// 填充评论者信息
	if author, err := model.GetUserByID(pgsql.DB, comment.UserID); err == nil {
		vo.AuthorName = author.Username
		vo.AuthorAvatar = author.AvatarURL
	}

	// 填充被回复人信息（直接从模型中的 ReplyToUserID 获取）
	if comment.ReplyToUserID > 0 {
		vo.ReplyToUserID = comment.ReplyToUserID
		if replyUser, err := model.GetUserByID(pgsql.DB, comment.ReplyToUserID); err == nil {
			vo.ReplyToName = replyUser.Username
		}
	}

	response.Success(c, vo)
}

// restorePostStatsIfNeed 恢复帖子统计信息到Redis缓存（如果缓存不存在）
func restorePostStatsIfNeed(postID int64) error {
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
