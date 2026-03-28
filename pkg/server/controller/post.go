package controller

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"
	redpanda "interestBar/pkg/server/storage/redpanda"
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
	Content    string                 `json:"content" binding:"omitempty,max=50000"`
	Summary    string                 `json:"summary" binding:"omitempty,max=500"`
	Type       int16                  `json:"type" binding:"omitempty,min=1,max=3"`
	MediaExtra []string `json:"media_extra" binding:"omitempty"`
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

	// 生成摘要：使用专门的工具从 Markdown 内容生成纯文本摘要
	// 如果用户提供了 summary，则使用用户的；否则从 content 自动生成
	summary := req.Summary
	if summary == "" && req.Content != "" {
		summary = utils.GenerateSummary(req.Content)
	}
	summary = strings.TrimSpace(summary)

	// 限制 summary 最大长度为 2000 字符（数据库字段限制）
	if len(summary) > 2001 {
		summary = string([]rune(summary)[:2001])
	}

	// 构建帖子数据模型
	post := model.Post{
		CircleID:   req.CircleID,
		UserID:     int64(userID),
		Type:       postType,
		Title:      strings.TrimSpace(req.Title),
		Summary:    summary,
		Content:    req.Content,
		MediaExtra: req.MediaExtra,
		Status:     postStatus,
		Deleted:    0,
	}

	// 如果没有提供 MediaExtra，设置为空数组
	if post.MediaExtra == nil {
		post.MediaExtra = make(model.MediaExtraJSON, 0)
	}

	// 创建帖子（会更新圈子的帖子计数）
	if err := model.CreatePost(pgsql.DB, &post); err != nil {
		response.InternalError(c, "Failed to create post")
		return
	}

	// 更新圈子帖子数量缓存（实时递增）
	if err := redispkg.IncrementCirclePostCount(post.CircleID); err != nil {
		// 缓存更新失败记录日志，但不影响主流程
		logger.Log.Error("Failed to increment circle post count: " + err.Error())
	}

	// 发送Redpanda消息用于持久化到数据库
	if err := redpanda.PublishCirclePostCount(post.CircleID); err != nil {
		// 仅记录日志，不影响主流程
		logger.Log.Error("Failed to publish post count message: " + err.Error())
	}

	// 返回创建成功消息和帖子ID
	response.SuccessWithMessage(c, "发帖成功", post.ID)
}

// PostDetailVO 帖子详情VO（包含Post所有字段 + 用户点赞状态 + 发帖人信息）
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

	// 发帖人信息
	AuthorID     int64  `json:"author_id"`     // 发帖人ID
	AuthorName   string `json:"author_name"`   // 发帖人用户昵称
	AuthorAvatar string `json:"author_avatar"` // 发帖人头像URL

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

	// 1. 获取帖子信息（不限制status，先查出来再判断权限）
	post, err := model.GetPostByID(pgsql.DB, postID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Post not found")
			return
		}
		response.InternalError(c, "Failed to get post")
		return
	}

	// 2. 权限检查：如果是作者本人，可以查看所有状态的帖子；如果是其他用户，只能查看已发布的帖子（status=1）
	if int64(userID) != post.UserID && post.Status != model.PostStatusPublished {
		response.NotFound(c, "Post not found")
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

	// 4. 查询发帖人信息
	var authorID int64 = post.UserID
	var authorName string
	var authorAvatar string

	author, err := model.GetUserByID(pgsql.DB, post.UserID)
	if err == nil && author != nil {
		authorID = author.ID
		authorName = author.Username
		authorAvatar = author.AvatarURL
	}

	// 5. 组装VO
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
		AuthorID:      authorID,
		AuthorName:    authorName,
		AuthorAvatar:  authorAvatar,
		IsLiked:       isLiked,
	}

	response.Success(c, vo)
}

// GetPostsRequest 获取帖子列表的请求结构
type GetPostsRequest struct {
	Keyword     string `form:"keyword"`      // 搜索关键字
	CircleID    int64  `form:"circle_id"`    // 圈子ID，为0时搜索所有圈子
	Size        int    `form:"size"`         // 每页数量，默认20
	SearchAfter string `form:"search_after"` // 上一页返回的search_after值（JSON字符串）
}

// GetPosts 获取帖子列表
// GET /post/list
func (ctrl *PostController) GetPosts(c *gin.Context) {
	// 解析请求参数
	var req GetPostsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 设置默认每页数量
	size := req.Size
	if size <= 0 || size > 100 {
		size = 20
	}

	// 解析 search_after 参数
	var searchAfter []interface{}
	if req.SearchAfter != "" {
		if err := json.Unmarshal([]byte(req.SearchAfter), &searchAfter); err != nil {
			response.BadRequest(c, "Invalid search_after parameter")
			return
		}
	}

	// 调用 Elasticsearch 搜索
	result, err := elasticsearch.SearchPosts(req.Keyword, req.CircleID, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search posts: " + err.Error())
		response.InternalError(c, "Failed to search posts")
		return
	}

	// 构建帖子列表VO，包含发帖人信息和圈子信息
	posts := make([]PostListVO, 0, len(result.Posts))

	// 批量收集所有的用户ID和圈子ID
	userIDs := make([]int64, 0, len(result.Posts))
	circleIDs := make([]int64, 0, len(result.Posts))
	userIDSet := make(map[int64]struct{})
	circleIDSet := make(map[int64]struct{})

	for _, doc := range result.Posts {
		if _, exists := userIDSet[doc.UserID]; !exists {
			userIDSet[doc.UserID] = struct{}{}
			userIDs = append(userIDs, doc.UserID)
		}
		if _, exists := circleIDSet[doc.CircleID]; !exists {
			circleIDSet[doc.CircleID] = struct{}{}
			circleIDs = append(circleIDs, doc.CircleID)
		}
	}

	// 批量查询用户信息和圈子信息
	userMap, _ := model.GetUsersByIDs(pgsql.DB, userIDs)
	circleMap, _ := model.GetCirclesByIDs(pgsql.DB, circleIDs)

	// 构建返回数据
	for _, doc := range result.Posts {
		// 从map中获取发帖人信息
		var authorName string
		var authorAvatar string

		if author, exists := userMap[doc.UserID]; exists {
			authorName = author.Username
			authorAvatar = author.AvatarURL
		}

		// 从map中获取圈子信息
		var circleName string
		var circleAvatar string

		if circle, exists := circleMap[doc.CircleID]; exists {
			circleName = circle.Name
			circleAvatar = circle.AvatarURL
		}

		// 解析时间字符串为 time.Time
		createTime, _ := time.Parse(time.RFC3339Nano, doc.CreateTime)

		post := PostListVO{
			ID:           doc.ID,
			CircleID:     doc.CircleID,
			UserID:       doc.UserID,
			Type:         doc.Type,
			Title:        doc.Title,
			Summary:      doc.Summary,
			Content:      doc.Content,
			ViewCount:    doc.ViewCount,
			CommentCount: doc.CommentCount,
			LikeCount:    doc.LikeCount,
			CollectCount: doc.CollectCount,
			IsPinned:     doc.IsPinned,
			IsEssence:    doc.IsEssence,
			IsLock:       doc.IsLock,
			Status:       doc.Status,
			CreateTime:   createTime,
			AuthorName:   authorName,
			AuthorAvatar: authorAvatar,
			CircleName:   circleName,
			CircleAvatar: circleAvatar,
		}
		posts = append(posts, post)
	}

	// 将 search_after 转换为 JSON 字符串返回
	var searchAfterJSON string
	if result.SearchAfter != nil {
		if bytes, err := json.Marshal(result.SearchAfter); err == nil {
			searchAfterJSON = string(bytes)
		}
	}

	// 构建响应数据
	responseData := map[string]interface{}{
		"posts":        posts,
		"total":        result.Total,
		"size":         result.Size,
		"search_after": searchAfterJSON,
	}

	response.Success(c, responseData)
}

// PostListVO 帖子列表VO（包含Post所有字段 + 发帖人信息）
type PostListVO struct {
	ID           int64     `json:"id"`
	CircleID     int64     `json:"circle_id"`
	UserID       int64     `json:"user_id"`
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

	// 发帖人信息
	AuthorName   string `json:"author_name"`   // 发帖人用户昵称
	AuthorAvatar string `json:"author_avatar"` // 发帖人头像URL

	// 圈子信息
	CircleName   string `json:"circle_name"`   // 圈子名称
	CircleAvatar string `json:"circle_avatar"` // 圈子头像URL
}
