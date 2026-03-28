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

	// 返回创建成功消息
	response.SuccessWithMessage(c, "创建圈子成功", nil)
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

	var circleBase redispkg.CircleBaseInfo

	// 1. 尝试从 Redis 获取圈子基础信息缓存（使用 Zstd 压缩）
	redisKey := redispkg.GetCircleInfoKey(circleID)
	err := redispkg.GetJSONCompressed(redisKey, &circleBase)
	if err != nil {
		// 缓存不存在或出错，从数据库查询完整信息
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

		// 从数据库的完整信息构建基础信息结构
		circleBase = redispkg.CircleBaseInfo{
			ID:          circle.ID,
			Name:        circle.Name,
			Slug:        circle.Slug,
			AvatarURL:   circle.AvatarURL,
			CoverURL:    circle.CoverURL,
			Description: circle.Description,
			Rule:        circle.Rule,
			CreatorID:   circle.CreatorID,
			CategoryID:  circle.CategoryID,
			JoinType:    circle.JoinType,
			Status:      circle.Status,
			Deleted:     circle.Deleted,
			CreateTime:  circle.CreateTime,
			UpdateTime:  circle.UpdateTime,
		}

		// 写入基础信息缓存（使用 Zstd 压缩，24小时过期）
		if err := redispkg.SetJSONCompressed(redisKey, &circleBase, 24*time.Hour); err != nil {
			logger.Log.Error("Failed to cache circle base info: " + err.Error())
		}
	}

	// 2. 从 Redis 获取统计信息（直接读取3个实时计数器）
	memberCount, postCount, hot, err := getCircleStatistics(circleID)
	if err != nil {
		logger.Log.Error("Failed to get circle statistics: " + err.Error())
		// 使用默认值
		memberCount = 0
		postCount = 0
		hot = 0
	}

	// 3. 查询用户在圈子的成员信息
	member, err := model.GetMember(pgsql.DB, circleID, int64(userID))
	if err != nil && err != gorm.ErrRecordNotFound {
		logger.Log.Error("Failed to get member info: " + err.Error())
		response.InternalError(c, "Failed to get member info")
		return
	}

	// 4. 组装VO
	vo := CircleDetailVO{
		ID:          circleBase.ID,
		Name:        circleBase.Name,
		Slug:        circleBase.Slug,
		AvatarURL:   circleBase.AvatarURL,
		CoverURL:    circleBase.CoverURL,
		Description: circleBase.Description,
		Rule:        circleBase.Rule,
		CreatorID:   circleBase.CreatorID,
		CategoryID:  circleBase.CategoryID,
		Hot:         hot,
		MemberCount: memberCount,
		PostCount:   postCount,
		JoinType:    circleBase.JoinType,
		Status:      circleBase.Status,
		Deleted:     circleBase.Deleted,
		CreateTime:  circleBase.CreateTime,
		UpdateTime:  circleBase.UpdateTime,
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

// JoinCircleRequest 加入圈子的请求结构
type JoinCircleRequest struct {
	CircleID int64 `json:"circle_id" binding:"required,min=1"`
}

// JoinCircle 加入兴趣圈
// POST /circle/join
func (ctrl *CircleController) JoinCircle(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req JoinCircleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 1. 检查圈子是否存在
	circle, err := model.GetCircleByID(pgsql.DB, req.CircleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Circle not found")
			return
		}
		logger.Log.Error("Failed to get circle: " + err.Error())
		response.InternalError(c, "Failed to get circle")
		return
	}

	// 2. 检查圈子状态
	if circle.Status != model.CircleStatusNormal {
		response.Forbidden(c, "This circle is not available for joining")
		return
	}

	// 3. 加入圈子
	member, err := model.JoinCircle(pgsql.DB, req.CircleID, int64(userID), circle.JoinType)
	if err != nil {
		logger.Log.Error("Failed to join circle: " + err.Error())
		if err.Error() == "user is already a member of this circle" {
			response.Conflict(c, "Already a member of this circle")
			return
		}
		if err.Error() == "this circle is private and requires invitation" {
			response.Forbidden(c, "This circle is private and requires invitation")
			return
		}
		response.InternalError(c, "Failed to join circle")
		return
	}

	// 4. 如果直接加入成功（不需要审核），立即更新Redis缓存并发送Redpanda消息持久化
	if member.Status == model.MemberStatusNormal {
		// 4.1 立即更新Redis缓存（实时计数，含缓存恢复逻辑）
		if err := incrementCircleMemberCount(req.CircleID); err != nil {
			// Redis更新失败记录日志，但不影响主流程
			logger.Log.Error("Failed to update Redis member count: " + err.Error())
		}

		// 4.2 发送Redpanda消息用于持久化到数据库
		if err := redpanda.PublishCircleMemberCount(req.CircleID, 1); err != nil {
			// 仅记录日志，不影响主流程
			logger.Log.Error("Failed to publish join message: " + err.Error())
		}

		// 4.3 删除用户已加入圈子缓存（旁路缓存）
		userJoinedKey := redispkg.GetUserJoinedCirclesKey(int64(userID))
		if err := redispkg.Del(userJoinedKey); err != nil {
			logger.Log.Error("Failed to delete user joined circles cache: " + err.Error())
		}
	}

	// 返回成功消息
	if member.Status == model.MemberStatusPending {
		response.SuccessWithMessage(c, "Join request submitted, awaiting approval", nil)
	} else {
		response.SuccessWithMessage(c, "Successfully joined the circle", nil)
	}
}

// LeaveCircleRequest 退出圈子的请求结构
type LeaveCircleRequest struct {
	CircleID int64 `json:"circle_id" binding:"required,min=1"`
}

// incrementCircleMemberCount 递增圈子成员计数（含缓存恢复逻辑）
func incrementCircleMemberCount(circleID int64) error {
	// 先检查统计信息Hash是否存在
	exists, err := redispkg.CircleStatisticsExists(circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
		// 即使检查失败，也尝试递增（让Redis自己处理）
	}

	// 如果统计信息不存在，从数据库恢复缓存
	if !exists {
		circle, err := model.GetCircleByID(pgsql.DB, circleID)
		if err != nil {
			// 数据库查询失败，记录日志但仍尝试递增（Redis会从0开始）
			logger.Log.Error(fmt.Sprintf("Failed to load circle %d from DB for cache recovery: %s", circleID, err.Error()))
		} else {
			// 将数据库的统计数据设置到 Redis Hash
			statistics := &redispkg.CircleStatistics{
				MemberCount: int(circle.MemberCount),
				PostCount:   int(circle.PostCount),
				Hot:         int(circle.Hot),
			}
			if err := redispkg.UpdateCircleStatistics(circleID, statistics); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}

	// 执行递增操作
	if err := redispkg.IncrementCircleMemberCount(circleID); err != nil {
		return fmt.Errorf("failed to increment member count: %w", err)
	}

	return nil
}

// decrementCircleMemberCount 递减圈子成员计数（含缓存恢复逻辑）
func decrementCircleMemberCount(circleID int64) error {
	// 先检查统计信息Hash是否存在
	exists, err := redispkg.CircleStatisticsExists(circleID)
	if err != nil {
		logger.Log.Error("Failed to check Redis statistics existence: " + err.Error())
		// 即使检查失败，也尝试递减（让Redis自己处理）
	}

	// 如果统计信息不存在，从数据库恢复缓存
	if !exists {
		circle, err := model.GetCircleByID(pgsql.DB, circleID)
		if err != nil {
			// 数据库查询失败，记录日志但仍尝试递减（Redis会从0开始变成-1然后重置为0）
			logger.Log.Error(fmt.Sprintf("Failed to load circle %d from DB for cache recovery: %s", circleID, err.Error()))
		} else {
			// 将数据库的统计数据设置到 Redis Hash
			statistics := &redispkg.CircleStatistics{
				MemberCount: int(circle.MemberCount),
				PostCount:   int(circle.PostCount),
				Hot:         int(circle.Hot),
			}
			if err := redispkg.UpdateCircleStatistics(circleID, statistics); err != nil {
				logger.Log.Error("Failed to restore Redis cache from DB: " + err.Error())
			}
		}
	}

	// 执行递减操作
	if err := redispkg.DecrementCircleMemberCount(circleID); err != nil {
		return fmt.Errorf("failed to decrement member count: %w", err)
	}

	return nil
}

// LeaveCircle 退出兴趣圈
// POST /circle/leave
func (ctrl *CircleController) LeaveCircle(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req LeaveCircleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	// 1. 检查圈子是否存在
	_, err := model.GetCircleByID(pgsql.DB, req.CircleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Circle not found")
			return
		}
		logger.Log.Error("Failed to get circle: " + err.Error())
		response.InternalError(c, "Failed to get circle")
		return
	}

	// 2. 检查是否是成员
	member, err := model.GetMember(pgsql.DB, req.CircleID, int64(userID))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			response.NotFound(c, "Not a member of this circle")
			return
		}
		logger.Log.Error("Failed to get member: " + err.Error())
		response.InternalError(c, "Failed to check membership")
		return
	}

	// 3. 退出圈子
	if err := model.LeaveCircle(pgsql.DB, req.CircleID, int64(userID)); err != nil {
		logger.Log.Error("Failed to leave circle: " + err.Error())
		if err.Error() == "circle owner cannot leave the circle" {
			response.Forbidden(c, "Circle owner cannot leave the circle")
			return
		}
		response.InternalError(c, "Failed to leave circle")
		return
	}

	// 4. 如果成员状态为正常，立即更新Redis缓存并发送Redpanda消息持久化
	if member.Status == model.MemberStatusNormal {
		// 4.1 立即更新Redis缓存（实时计数，含缓存恢复逻辑）
		if err := decrementCircleMemberCount(req.CircleID); err != nil {
			// Redis更新失败记录日志，但不影响主流程
			logger.Log.Error("Failed to update Redis member count: " + err.Error())
		}

		// 4.2 发送Redpanda消息用于持久化到数据库
		if err := redpanda.PublishCircleMemberCount(req.CircleID, -1); err != nil {
			// 仅记录日志，不影响主流程
			logger.Log.Error("Failed to publish leave message: " + err.Error())
		}

		// 4.3 删除用户已加入圈子缓存（旁路缓存）
		userJoinedKey := redispkg.GetUserJoinedCirclesKey(int64(userID))
		if err := redispkg.Del(userJoinedKey); err != nil {
			logger.Log.Error("Failed to delete user joined circles cache: " + err.Error())
		}
	}

	response.SuccessWithMessage(c, "Successfully left the circle", nil)
}

// getCircleStatistics 从Redis获取圈子统计信息
// 如果任一计数器不存在，则从数据库恢复所有计数器
func getCircleStatistics(circleID int64) (memberCount, postCount, hot int, err error) {
	// 从Hash读取统计信息
	stats, err := redispkg.GetCircleStatistics(circleID)
	if err != nil {
		logger.Log.Error("Failed to get circle statistics: " + err.Error())
		return 0, 0, 0, err
	}

	// 如果统计信息不存在，从数据库恢复
	if stats == nil {
		logger.Log.Debug(fmt.Sprintf("Circle statistics cache missing for circle %d, restoring from database", circleID))
		return restoreAllCounters(circleID)
	}

	return stats.MemberCount, stats.PostCount, stats.Hot, nil
}

// restoreAllCounters 从数据库重新加载并重建Hash中的所有统计字段
func restoreAllCounters(circleID int64) (memberCount, postCount, hot int, err error) {
	// 从数据库查询完整circle记录
	circle, err := model.GetCircleByID(pgsql.DB, circleID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, 0, 0, fmt.Errorf("circle not found: %d", circleID)
		}
		return 0, 0, 0, fmt.Errorf("failed to get circle from database: %w", err)
	}

	// 使用Hash批量设置所有统计字段
	statistics := &redispkg.CircleStatistics{
		MemberCount: int(circle.MemberCount),
		PostCount:   int(circle.PostCount),
		Hot:         int(circle.Hot),
	}
	if err := redispkg.UpdateCircleStatistics(circleID, statistics); err != nil {
		logger.Log.Error("Failed to restore circle statistics cache: " + err.Error())
	}

	logger.Log.Debug(fmt.Sprintf("Successfully restored circle statistics cache for circle %d: member_count=%d, post_count=%d, hot=%d",
		circleID, circle.MemberCount, circle.PostCount, circle.Hot))

	return int(circle.MemberCount), int(circle.PostCount), int(circle.Hot), nil
}

// MyCircleVO 我加入的圈子VO（简化版，只包含基本信息）
type MyCircleVO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	MemberCount int    `json:"member_count"`
}

// GetMyCirclesRequest 获取我加入圈子列表的请求结构
type GetMyCirclesRequest struct {
	Keyword     string `form:"keyword"`      // 搜索关键字
	Size        int    `form:"size"`         // 每页数量，默认20
	SearchAfter string `form:"search_after"` // 上一页返回的search_after值（JSON字符串）
}

// GetMyCircles 获取我加入的圈子列表
// GET /circle/my
func (ctrl *CircleController) GetMyCircles(c *gin.Context) {
	// 获取当前登录用户ID
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	// 解析请求参数
	var req GetMyCirclesRequest
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

	var circleIDs []int64
	redisKey := redispkg.GetUserJoinedCirclesKey(int64(userID))

	// 1. 尝试从Redis获取已加入圈子ID列表缓存
	err := redispkg.GetJSON(redisKey, &circleIDs)
	if err != nil {
		// 缓存不存在或出错，从数据库查询（缓存恢复）
		circleIDs, err = model.GetJoinedCircleIDsByUserID(pgsql.DB, int64(userID))
		if err != nil {
			logger.Log.Error("Failed to get joined circles: " + err.Error())
			response.InternalError(c, "Failed to get joined circles")
			return
		}

		// 写入缓存（24小时过期）
		if err := redispkg.SetJSON(redisKey, circleIDs, 24*time.Hour); err != nil {
			logger.Log.Error("Failed to cache joined circle IDs: " + err.Error())
		}
	}

	// 如果没有加入任何圈子，直接返回空结果
	if len(circleIDs) == 0 {
		response.Success(c, map[string]interface{}{
			"circles":      []MyCircleVO{},
			"total":        0,
			"size":         size,
			"search_after": nil,
		})
		return
	}

	// 2. 调用 Elasticsearch 搜索已加入的圈子
	result, err := elasticsearch.SearchMyCircles(circleIDs, req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search my circles: " + err.Error())
		response.InternalError(c, "Failed to search my circles")
		return
	}

	// 3. 将 ES 结果转换为 MyCircleVO
	circles := make([]MyCircleVO, 0, len(result.Circles))
	for _, doc := range result.Circles {
		circles = append(circles, MyCircleVO{
			ID:          doc.ID,
			Name:        doc.Name,
			AvatarURL:   doc.AvatarURL,
			MemberCount: doc.MemberCount,
		})
	}

	// 4. 将 search_after 转换为 JSON 字符串返回
	var searchAfterJSON string
	if result.SearchAfter != nil {
		if bytes, err := json.Marshal(result.SearchAfter); err == nil {
			searchAfterJSON = string(bytes)
		}
	}

	// 构建响应数据
	response.Success(c, map[string]interface{}{
		"circles":      circles,
		"total":        result.Total,
		"size":         result.Size,
		"search_after": searchAfterJSON,
	})
}
