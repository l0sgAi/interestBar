// Package http 提供 circle 领域的 HTTP 入站适配器（handler + 路由注册）。
package http

import (
	"encoding/json"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/domains/circle/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 circle 相关的 HTTP 请求。
type Handler struct {
	svc application.CircleService
}

// NewHandler 构造 circle Handler。
func NewHandler(svc application.CircleService) *Handler {
	return &Handler{svc: svc}
}

// CreateCircleRequest 创建圈子请求。
type CreateCircleRequest struct {
	Name        string    `json:"name" binding:"required,min=1,max=50"`
	Slug        string    `json:"slug" binding:"omitempty,max=60"`
	AvatarURL   string    `json:"avatar_url" binding:"omitempty,url"`
	CoverURL    string    `json:"cover_url" binding:"omitempty,url"`
	Description string    `json:"description" binding:"required,min=1,max=2000"`
	Rule        string    `json:"rule" binding:"omitempty,max=2000"`
	CategoryID  uuid.UUID `json:"category_id" binding:"required"`
	JoinType    int16     `json:"join_type" binding:"omitempty,min=0,max=2"`
}

// CreateCircle POST /circle/create
func (h *Handler) CreateCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req CreateCircleRequest
	if err := c.BindJSON(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters:")
		return
	}

	err := h.svc.CreateCircle(c, userID, application.CreateCircleInput{
		Name: req.Name, Slug: req.Slug, AvatarURL: req.AvatarURL,
		CoverURL: req.CoverURL, Description: req.Description, Rule: req.Rule,
		CategoryID: req.CategoryID, JoinType: req.JoinType,
	})
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "创建圈子成功", nil)
}

// GetCirclesRequest 获取圈子列表请求。
type GetCirclesRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetCircles GET /circle/list
func (h *Handler) GetCircles(c appctx.AppContext) {
	var req GetCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)
	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.SearchCircles(c, req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search circles: " + err.Error())
		httputil.InternalError(c, "Failed to search circles")
		return
	}
	httputil.Success(c, result)
}

// GetCircleDetail GET /circle/detail/:id
func (h *Handler) GetCircleDetail(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	circleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid circle id")
		return
	}

	vo, err := h.svc.GetCircleDetail(c, userID, circleID)
	if err != nil {
		if err == domain.ErrCircleNotFound {
			httputil.NotFound(c, "Circle not found")
			return
		}
		logger.Log.Error("Failed to get circle: " + err.Error())
		httputil.InternalError(c, "Failed to get circle")
		return
	}
	httputil.Success(c, vo)
}

// JoinCircleRequest 加入圈子请求。
type JoinCircleRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
}

// JoinCircle POST /circle/join
func (h *Handler) JoinCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req JoinCircleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	joined, pending, err := h.svc.JoinCircle(c, userID, req.CircleID)
	if err != nil {
		writeCircleError(c, err)
		return
	}

	if pending {
		httputil.SuccessWithMessage(c, "Join request submitted, awaiting approval", nil)
		return
	}
	_ = joined
	httputil.SuccessWithMessage(c, "Successfully joined the circle", nil)
}

// LeaveCircleRequest 退出圈子请求。
type LeaveCircleRequest struct {
	CircleID uuid.UUID `json:"circle_id" binding:"required"`
}

// LeaveCircle POST /circle/leave
func (h *Handler) LeaveCircle(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req LeaveCircleRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	if err := h.svc.LeaveCircle(c, userID, req.CircleID); err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "Successfully left the circle", nil)
}

// GetMyCirclesRequest 我加入圈子列表请求。
type GetMyCirclesRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetMyCircles GET /circle/my
func (h *Handler) GetMyCircles(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetMyCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	size := normalizeSize(req.Size)

	result, err := h.svc.GetMyCircles(c, userID, req.Keyword, size, req.SearchAfter)
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.Success(c, result)
}

// GetUserCirclesRequest 任意用户加入圈子列表请求。
type GetUserCirclesRequest struct {
	UserID      string `query:"user_id"`
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetUserCircles GET /circle/user —— 查看任意用户加入的圈子（分页）。
func (h *Handler) GetUserCircles(c appctx.AppContext) {
	var req GetUserCirclesRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	targetUserID, err := uuid.Parse(req.UserID)
	if err != nil {
		httputil.BadRequest(c, "Invalid user_id")
		return
	}

	size := normalizeSize(req.Size)

	result, err := h.svc.GetUserCircles(c, targetUserID, req.Keyword, size, req.SearchAfter)
	if err != nil {
		writeCircleError(c, err)
		return
	}
	httputil.Success(c, result)
}

// GetCirclePostsRequest 圈内帖子列表请求。
type GetCirclePostsRequest struct {
	CircleID    string `query:"circle_id" binding:"required,uuid"`
	Type        int    `query:"type" binding:"required,min=1,max=3"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetCirclePosts GET /circle/posts
func (h *Handler) GetCirclePosts(c appctx.AppContext) {
	var req GetCirclePostsRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	circleID, err := uuid.Parse(req.CircleID)
	if err != nil {
		httputil.BadRequest(c, "Invalid circle_id")
		return
	}

	size := normalizeSize(req.Size)
	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.GetCirclePosts(c, circleID, req.Type, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search circle posts: " + err.Error())
		httputil.InternalError(c, "Failed to search circle posts")
		return
	}
	httputil.Success(c, result)
}

// ===== 辅助函数 =====

func requireUserID(c appctx.AppContext) (uuid.UUID, bool) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		httputil.Unauthorized(c, "Token not found")
		return uuid.Nil, false
	}
	userID, err := uuid.Parse(loginID)
	if err != nil {
		httputil.BadRequest(c, "Invalid user ID")
		return uuid.Nil, false
	}
	return userID, true
}

func normalizeSize(size int) int {
	if size <= 0 || size > 100 {
		return 20
	}
	return size
}

// parseSearchAfter 解析 search_after 参数。非法时写入 BadRequest 并返回 ok=false。
func parseSearchAfter(c appctx.AppContext, s string) ([]interface{}, bool) {
	if s == "" {
		return nil, true
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		httputil.BadRequest(c, "Invalid search_after parameter")
		return nil, false
	}
	return arr, true
}

// writeCircleError 把 service 层错误映射到 HTTP 响应。
func writeCircleError(c appctx.AppContext, err error) {
	switch {
	case application.IsInvalidCursorErr(err):
		httputil.BadRequest(c, "Invalid search_after parameter")
	case application.IsInvalidJoinTypeErr(err):
		httputil.BadRequest(c, "join_type must be 0 (direct), 1 (approval), or 2 (private)")
	case application.IsCircleNameExistsErr(err):
		httputil.Conflict(c, "Circle name already exists")
	case application.IsCircleSlugExistsErr(err):
		httputil.Conflict(c, "Circle slug already exists")
	case application.IsCircleNotAvailableErr(err):
		httputil.Forbidden(c, "This circle is not available for joining")
	case application.IsAlreadyMemberErr(err):
		httputil.Conflict(c, "Already a member of this circle")
	case application.IsBannedFromCircleErr(err):
		httputil.Forbidden(c, "User is banned from this circle")
	case application.IsPrivateCircleErr(err):
		httputil.Forbidden(c, "This circle is private and requires invitation")
	case application.IsOwnerCannotLeaveErr(err):
		httputil.Forbidden(c, "Circle owner cannot leave the circle")
	case application.IsNotMemberErr(err):
		httputil.NotFound(c, "Not a member of this circle")
	case err == domain.ErrCircleNotFound:
		httputil.NotFound(c, "Circle not found")
	case err == domain.ErrMemberNotFound:
		httputil.NotFound(c, "Not a member of this circle")
	default:
		logger.Log.Error("circle service error: " + err.Error())
		httputil.InternalError(c, "Internal error")
	}
}
