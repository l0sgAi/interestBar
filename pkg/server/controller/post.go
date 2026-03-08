package controller

import (
	"fmt"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PostController 处理帖子相关操作
type PostController struct{}

func NewPostController() *PostController {
	return &PostController{}
}

// CreatePostRequest 创建帖子的请求结构
type CreatePostRequest struct {
	CircleID   int64                  `json:"circle_id" binding:"required,min=1"`
	Title      string                 `json:"title" binding:"required,min=1,max=200"`
	Content    string                 `json:"content" binding:"omitempty,max=10000"`
	Summary    string                 `json:"summary" binding:"omitempty,max=500"`
	Type       int16                  `json:"type" binding:"omitempty,min=1,max=3"`
	MediaExtra map[string]interface{} `json:"media_extra" binding:"omitempty"`
	Status     int16                  `json:"status" binding:"omitempty,min=0,max=4"`
}

// CreatePost 创建帖子
// POST /post/create
func (ctrl *PostController) CreatePost(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 检查帖子类型，默认为1（图文）
	postType := req.Type
	if postType == 0 {
		postType = model.PostTypeTextImage
	}

	// 检查帖子状态，默认为2（审核中）
	postStatus := req.Status
	if postStatus == 0 {
		postStatus = model.PostStatusReviewing
	}

	// 如果是草稿，不限制标题和内容
	if postStatus != model.PostStatusDraft {
		// 检查圈子ID和标题不能为空
		if req.CircleID == 0 {
			response.BadRequest(c, "circle_id is required")
			return
		}
		if req.Title == "" {
			response.BadRequest(c, "title is required")
			return
		}
	}

	// 1. 检查是否为圈子成员
	member, err := model.GetMember(pgsql.DB, req.CircleID, int64(userID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Forbidden(c, "You are not a member of this circle")
			return
		}
		response.InternalError(c, "Failed to check membership")
		return
	}

	// 2. 检查成员状态
	if member.Status != model.MemberStatusNormal {
		switch member.Status {
		case model.MemberStatusPending:
			response.Forbidden(c, "Your membership is still pending approval")
			return
		case model.MemberStatusMuted:
			// 检查禁言是否已过期
			if member.MuteEndTime != nil && member.MuteEndTime.After(time.Now()) {
				response.Forbidden(c, "You are muted until "+member.MuteEndTime.Format("2006-01-02 15:04:05"))
				return
			}
		case model.MemberStatusBanned:
			response.Forbidden(c, "You have been banned from this circle")
			return
		}
	}

	// 3. 检查圈子是否存在
	circle, err := model.GetCircleByID(pgsql.DB, req.CircleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Circle not found")
			return
		}
		response.InternalError(c, "Failed to check circle")
		return
	}

	// 检查圈子状态
	if circle.Status != model.CircleStatusNormal {
		response.Forbidden(c, "This circle is not available for posting")
		return
	}

	// 构建帖子数据模型
	post := model.Post{
		CircleID:   req.CircleID,
		UserID:     int64(userID),
		Type:       postType,
		Title:      strings.TrimSpace(req.Title),
		Summary:    strings.TrimSpace(req.Summary),
		Content:    req.Content,
		MediaExtra: req.MediaExtra,
		Status:     postStatus,
		Deleted:    0,
	}

	// 如果没有提供 MediaExtra，设置为空 map
	if post.MediaExtra == nil {
		post.MediaExtra = make(model.MediaExtraJSON)
	}

	// 创建帖子（会更新圈子的帖子计数）
	if err := model.CreatePost(pgsql.DB, &post); err != nil {
		response.InternalError(c, "Failed to create post")
		return
	}

	// 删除圈子信息缓存（因为圈子包含帖子数量统计）
	circleCacheKey := redispkg.GetCircleInfoKey(post.CircleID)
	if err := redispkg.Del(circleCacheKey); err != nil {
		// 缓存删除失败记录日志，但不影响主流程
		logger.Log.Error("Failed to delete circle cache: " + err.Error())
	}

	// 返回创建成功消息和帖子ID
	response.SuccessWithMessage(c, "发帖成功", post.ID)
}

// PostDetailVO 帖子详情VO（包含Post所有字段 + 用户点赞状态）
type PostDetailVO struct {
	// Post 所有字段
	ID            int64                `json:"id"`
	CircleID      int64                `json:"circle_id"`
	UserID        int64                `json:"user_id"`
	Type          int16                `json:"type"`
	Title         string               `json:"title"`
	Summary       string               `json:"summary"`
	Content       string               `json:"content"`
	MediaExtra    model.MediaExtraJSON `json:"media_extra"`
	ViewCount     int                  `json:"view_count"`
	CommentCount  int                  `json:"comment_count"`
	LikeCount     int                  `json:"like_count"`
	CollectCount  int                  `json:"collect_count"`
	IsPinned      int16                `json:"is_pinned"`
	IsEssence     int16                `json:"is_essence"`
	IsLock        int16                `json:"is_lock"`
	Status        int16                `json:"status"`
	Deleted       int16                `json:"deleted"`
	CreateTime    time.Time            `json:"create_time"`
	UpdateTime    time.Time            `json:"update_time"`
	LastReplyTime *time.Time           `json:"last_reply_time,omitempty"`

	// 用户交互状态
	IsLiked bool `json:"is_liked"` // 当前用户是否点赞了该帖子
}

// GetPostDetail 获取帖子详情
// GET /post/detail/:id
func (ctrl *PostController) GetPostDetail(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 获取post_id参数
	postIDStr := c.Param("id")
	var postID int64
	if _, err := fmt.Sscanf(postIDStr, "%d", &postID); err != nil || postID <= 0 {
		response.BadRequest(c, "Invalid post id")
		return
	}

	// 1. 获取帖子信息
	post, err := model.GetPostByID(pgsql.DB, postID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Post not found")
			return
		}
		response.InternalError(c, "Failed to get post")
		return
	}

	// 2. 检查用户是否点赞了该帖子
	isLiked, err := model.IsPostLiked(pgsql.DB, int64(userID), postID)
	if err != nil {
		// 点赞状态查询失败不影响接口返回，默认为false
		isLiked = false
	}

	// TODO: 3. 异步增加浏览量（不阻塞主流程）
	// go func() {
	// 	_ = model.IncrementViewCount(pgsql.DB, postID)
	// }()

	// 4. 组装VO
	vo := PostDetailVO{
		ID:            post.ID,
		CircleID:      post.CircleID,
		UserID:        post.UserID,
		Type:          post.Type,
		Title:         post.Title,
		Summary:       post.Summary,
		Content:       post.Content,
		MediaExtra:    post.MediaExtra,
		ViewCount:     post.ViewCount,
		CommentCount:  post.CommentCount,
		LikeCount:     post.LikeCount,
		CollectCount:  post.CollectCount,
		IsPinned:      post.IsPinned,
		IsEssence:     post.IsEssence,
		IsLock:        post.IsLock,
		Status:        post.Status,
		Deleted:       post.Deleted,
		CreateTime:    post.CreateTime,
		UpdateTime:    post.UpdateTime,
		LastReplyTime: post.LastReplyTime,
		IsLiked:       isLiked,
	}

	response.Success(c, vo)
}
