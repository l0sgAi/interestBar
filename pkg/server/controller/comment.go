package controller

import (
	"fmt"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
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

	// 2. 如果是回复，校验 root_id 和 reply_to_id
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

		// 如果指定了 reply_to_id，校验被回复的评论存在
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
		}
	}

	// 3. 构建评论数据
	comment := model.Comment{
		PostID:    req.PostID,
		UserID:    int64(userID),
		RootID:    req.RootID,
		ReplyToID: req.ReplyToID,
		Content:   req.Content,
		Status:    model.CommentStatusNormal,
		Deleted:   0,
	}

	// 4. 创建评论（事务：插入评论 + 更新帖子评论计数 + 更新根评论回复计数）
	if err := model.CreateComment(pgsql.DB, &comment); err != nil {
		response.InternalError(c, "Failed to create comment")
		return
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
	ReplyToName string `json:"reply_to_name,omitempty"`
}

// GetCommentsRequest 获取评论列表的请求结构
type GetCommentsRequest struct {
	PostID   int64 `form:"post_id" binding:"required,min=1"`
	RootID   int64 `form:"root_id" binding:"omitempty"`       // 0=获取顶层评论, >0=获取该根评论下的回复
	Page     int   `form:"page" binding:"omitempty,min=1"`
	PageSize int   `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// GetComments 获取评论列表
// GET /comment/list
func (ctrl *CommentController) GetComments(c *gin.Context) {
	// 解析请求参数
	var req GetCommentsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 默认分页
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	var comments []model.Comment
	var total int64
	var err error

	if req.RootID > 0 {
		// 获取子回复
		comments, total, err = model.GetSubCommentsByRoot(pgsql.DB, req.RootID, page, pageSize)
	} else {
		// 获取顶层评论
		comments, total, err = model.GetRootCommentsByPost(pgsql.DB, req.PostID, page, pageSize)
	}

	if err != nil {
		response.InternalError(c, "Failed to get comments")
		return
	}

	// 批量查询评论者信息
	userIDs := make([]int64, 0, len(comments))
	userIDSet := make(map[int64]struct{})
	for _, cm := range comments {
		if _, exists := userIDSet[cm.UserID]; !exists {
			userIDSet[cm.UserID] = struct{}{}
			userIDs = append(userIDs, cm.UserID)
		}
	}
	userMap, _ := model.GetUsersByIDs(pgsql.DB, userIDs)

	// 收集 reply_to_id 对应的用户
	replyUserIDs := make([]int64, 0)
	replyUserIDSet := make(map[int64]struct{})
	for _, cm := range comments {
		if cm.ReplyToID > 0 {
			if _, exists := replyUserIDSet[cm.ReplyToID]; !exists {
				replyUserIDSet[cm.ReplyToID] = struct{}{}
				replyUserIDs = append(replyUserIDs, cm.ReplyToID)
			}
		}
	}

	// 查询被回复评论的作者
	var replyUserMap map[int64]*model.SysUser
	if len(replyUserIDs) > 0 {
		replyUserMap, _ = model.GetUsersByIDs(pgsql.DB, replyUserIDs)
	} else {
		replyUserMap = make(map[int64]*model.SysUser)
	}

	// 构建VO
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

		// 填充被回复人信息
		if cm.ReplyToID > 0 {
			if replyUser, exists := replyUserMap[cm.ReplyToID]; exists {
				vo.ReplyToName = replyUser.Username
			}
		}

		vos = append(vos, vo)
	}

	response.Pagination(c, vos, total, page, pageSize)
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

	response.Success(c, vo)
}
