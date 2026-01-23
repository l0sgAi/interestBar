package controller

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	elasticsearch "interestBar/pkg/server/storage/elasticsearch"
	rabbitmq "interestBar/pkg/server/storage/rabbitmq"
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
	Rule        string `json:"rule" binding:"omitempty,max=2000"` // 圈子规则/公告，最多5000字符
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
		logger.Log.Error("Invalid request parameters: " + err.Error())
		response.BadRequest(c, "Invalid request parameters:")
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
		Rule:        strings.TrimSpace(req.Rule),
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
		logger.Log.Error("Failed to create circle: " + err.Error())
		response.InternalError(c, "Failed to create circle")
		return
	}

	// 异步同步到 Elasticsearch（通过 RabbitMQ）
	createTime := circle.CreateTime.Format("2006-01-02T15:04:05Z07:00")
	if err := rabbitmq.PublishCircleSync(
		rabbitmq.CircleSyncActionCreate,
		circle.ID,
		circle.Name,
		circle.AvatarURL,
		circle.Description,
		circle.Hot,
		circle.CategoryID,
		circle.MemberCount,
		circle.PostCount,
		createTime,
		circle.Status,
		circle.Deleted,
		circle.JoinType,
	); err != nil {
		// 仅记录日志，不影响主流程
		logger.Log.Error("Failed to publish circle sync message: " + err.Error())
	}

	// 返回创建成功消息
	response.SuccessWithMessage(c, "创建圈子成功", nil)
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

	// 返回创建成功消息
	response.SuccessWithMessage(c, "发帖成功", nil)
}

// GetCirclesRequest 获取圈子列表的请求结构
type GetCirclesRequest struct {
	Keyword     string `form:"keyword"`      // 搜索关键字
	Size        int    `form:"size"`         // 每页数量，默认20
	SearchAfter string `form:"search_after"` // 上一页返回的search_after值（JSON字符串）
}

// GetCircles 获取圈子列表
// GET /circle/list
func (ctrl *CircleController) GetCircles(c *gin.Context) {
	// 解析请求参数
	var req GetCirclesRequest
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
	result, err := elasticsearch.SearchCircles(req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search circles: " + err.Error())
		response.InternalError(c, "Failed to search circles")
		return
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
		"circles":      result.Circles,
		"total":        result.Total,
		"size":         result.Size,
		"search_after": searchAfterJSON,
	}

	response.Success(c, responseData)
}

// CircleDetailVO 兴趣圈详情VO（包含Circle所有字段 + 用户成员信息）
type CircleDetailVO struct {
	// Circle 所有字段
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	Description string    `json:"description"`
	Rule        string    `json:"rule,omitempty"`
	CreatorID   int64     `json:"creator_id"`
	CategoryID  int       `json:"category_id"`
	Hot         int       `json:"hot"`
	MemberCount int       `json:"member_count"`
	PostCount   int       `json:"post_count"`
	JoinType    int16     `json:"join_type"`
	Status      int16     `json:"status"`
	Deleted     int16     `json:"deleted"`
	CreateTime  time.Time `json:"create_time"`
	UpdateTime  time.Time `json:"update_time"`

	// 用户在圈子的成员信息
	IsJoined          bool       `json:"is_joined"`                      // 是否已加入圈子
	MemberRole        int16      `json:"member_role,omitempty"`          // 角色
	MemberStatus      int16      `json:"member_status,omitempty"`        // 成员状态
	MemberMuteEndTime *time.Time `json:"member_mute_end_time,omitempty"` // 禁言结束时间
	MemberIsTop       int16      `json:"member_is_top,omitempty"`        // 是否置顶显示
	MemberIsDisturb   int16      `json:"member_is_disturb,omitempty"`    // 消息免打扰
}

// GetCircleDetail 获取兴趣圈详情
// GET /circle/detail/:id
func (ctrl *CircleController) GetCircleDetail(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 获取circle_id参数
	circleIDStr := c.Param("id")
	var circleID int64
	if _, err := fmt.Sscanf(circleIDStr, "%d", &circleID); err != nil || circleID <= 0 {
		response.BadRequest(c, "Invalid circle id")
		return
	}

	// 1. 查询circle基本信息
	circle, err := model.GetCircleByID(pgsql.DB, circleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Circle not found")
			return
		}
		logger.Log.Error("Failed to get circle: " + err.Error())
		response.InternalError(c, "Failed to get circle")
		return
	}

	// 2. 查询用户在圈子的成员信息
	member, err := model.GetMember(pgsql.DB, circleID, int64(userID))
	if err != nil && err != gorm.ErrRecordNotFound {
		logger.Log.Error("Failed to get member info: " + err.Error())
		response.InternalError(c, "Failed to get member info")
		return
	}

	// 3. 组装VO
	vo := CircleDetailVO{
		ID:          circle.ID,
		Name:        circle.Name,
		Slug:        circle.Slug,
		AvatarURL:   circle.AvatarURL,
		CoverURL:    circle.CoverURL,
		Description: circle.Description,
		Rule:        circle.Rule,
		CreatorID:   circle.CreatorID,
		CategoryID:  circle.CategoryID,
		Hot:         circle.Hot,
		MemberCount: circle.MemberCount,
		PostCount:   circle.PostCount,
		JoinType:    circle.JoinType,
		Status:      circle.Status,
		Deleted:     circle.Deleted,
		CreateTime:  circle.CreateTime,
		UpdateTime:  circle.UpdateTime,
	}

	// 如果用户是圈子成员，添加成员信息
	if member != nil {
		vo.IsJoined = true
		vo.MemberRole = member.Role
		vo.MemberStatus = member.Status
		vo.MemberMuteEndTime = member.MuteEndTime
		vo.MemberIsTop = member.IsTop
		vo.MemberIsDisturb = member.IsDisturb
	} else {
		vo.IsJoined = false
	}

	response.Success(c, vo)
}
