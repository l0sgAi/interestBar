package controller

import (
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/utils"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CircleController 处理圈子相关操作
type CircleController struct{}

func NewCircleController() *CircleController {
	return &CircleController{}
}

// CreateCircleRequest 创建圈子的请求结构
type CreateCircleRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=50"`
	Slug        string `json:"slug" binding:"omitempty,max=60"`
	AvatarURL   string `json:"avatar_url" binding:"omitempty,url"`
	CoverURL    string `json:"cover_url" binding:"omitempty,url"`
	Description string `json:"description" binding:"required,min=1,max=2000"`
	CategoryID  int    `json:"category_id" binding:"required,min=0"`
	JoinType    int16  `json:"join_type" binding:"omitempty,min=0,max=2"`
}

// CreateCircle 创建兴趣圈
// POST /circle/create
func (ctrl *CircleController) CreateCircle(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req CreateCircleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters: "+err.Error())
		return
	}

	// 验证 join_type，如果未指定默认为0（直接加入）
	if req.JoinType < 0 || req.JoinType > 2 {
		response.BadRequest(c, "join_type must be 0 (direct), 1 (approval), or 2 (private)")
		return
	}

	// 检查圈子名称是否已存在（只检查未删除的）
	var existingCircle model.Circle
	checkResult := pgsql.DB.Where("name = ? AND deleted = ?", req.Name, 0).First(&existingCircle)
	if checkResult.Error == nil {
		// 找到同名圈子
		response.Conflict(c, "Circle name already exists")
		return
	}
	if checkResult.Error != gorm.ErrRecordNotFound {
		// 数据库查询错误
		response.InternalError(c, "Failed to check circle name")
		return
	}

	// 如果提供了 slug，检查 slug 是否已存在
	if req.Slug != "" {
		slug := strings.TrimSpace(req.Slug)
		var existingSlug model.Circle
		checkSlugResult := pgsql.DB.Where("slug = ? AND deleted = ?", slug, 0).First(&existingSlug)
		if checkSlugResult.Error == nil {
			response.Conflict(c, "Circle slug already exists")
			return
		}
		if checkSlugResult.Error != gorm.ErrRecordNotFound {
			response.InternalError(c, "Failed to check circle slug")
			return
		}
	}

	// 构建圈子数据模型
	circle := model.Circle{
		Name:        strings.TrimSpace(req.Name),
		Slug:        strings.TrimSpace(req.Slug),
		AvatarURL:   req.AvatarURL,
		CoverURL:    req.CoverURL,
		Description: strings.TrimSpace(req.Description),
		CreatorID:   int64(userID),
		CategoryID:  req.CategoryID,
		Hot:         0,
		MemberCount: 1, // 创建者自动成为第一个成员
		PostCount:   0,
		JoinType:    req.JoinType,
		Status:      model.CircleStatusNormal, // 默认状态为正常
		Deleted:     0,
	}

	// 使用事务创建圈子并添加创建者为圈主
	if err := model.CreateCircle(pgsql.DB, &circle); err != nil {
		response.InternalError(c, "Failed to create circle: "+err.Error())
		return
	}

	// 返回创建的圈子信息
	response.Created(c, gin.H{
		"id":           circle.ID,
		"name":         circle.Name,
		"slug":         circle.Slug,
		"avatar_url":   circle.AvatarURL,
		"cover_url":    circle.CoverURL,
		"description":  circle.Description,
		"creator_id":   circle.CreatorID,
		"category_id":  circle.CategoryID,
		"member_count": circle.MemberCount,
		"join_type":    circle.JoinType,
		"status":       circle.Status,
		"create_time":  circle.CreateTime,
	})
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
// POST /circle/post/create
func (ctrl *CircleController) CreatePost(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters: "+err.Error())
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
		response.InternalError(c, "Failed to create post: "+err.Error())
		return
	}

	// 返回创建的帖子信息
	response.Created(c, gin.H{
		"id":          post.ID,
		"circle_id":   post.CircleID,
		"user_id":     post.UserID,
		"type":        post.Type,
		"title":       post.Title,
		"summary":     post.Summary,
		"status":      post.Status,
		"create_time": post.CreateTime,
	})
}
