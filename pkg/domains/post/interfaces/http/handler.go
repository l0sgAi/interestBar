// Package http 提供 post 领域的 HTTP 入站适配器。
package http

import (
	"encoding/json"

	"interestBar/pkg/domains/post/application"
	"interestBar/pkg/domains/post/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 post 相关的 HTTP 请求。
type Handler struct {
	svc application.PostService
}

// NewHandler 构造 post Handler。
func NewHandler(svc application.PostService) *Handler {
	return &Handler{svc: svc}
}

// CreatePostRequest 创建帖子请求。
type CreatePostRequest struct {
	CircleID   uuid.UUID `json:"circle_id" binding:"required"`
	Title      string    `json:"title" binding:"required,min=1,max=200"`
	Content    string    `json:"content" binding:"omitempty,max=50000"`
	Summary    string    `json:"summary" binding:"omitempty,max=500"`
	Type       int16     `json:"type" binding:"omitempty,min=1,max=3"`
	MediaExtra []string  `json:"media_extra" binding:"omitempty"`
	Status     int16     `json:"status" binding:"omitempty,min=0,max=4"`
}

// CreatePost POST /post/create
func (h *Handler) CreatePost(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req CreatePostRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	postID, err := h.svc.CreatePost(c, userID, application.CreatePostInput{
		CircleID: req.CircleID, Title: req.Title, Content: req.Content,
		Summary: req.Summary, Type: req.Type, MediaExtra: req.MediaExtra, Status: req.Status,
	})
	if err != nil {
		writePostError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "发帖成功", postID)
}

// GetPostDetail GET /post/detail/:id
func (h *Handler) GetPostDetail(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	postID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid post id")
		return
	}

	vo, err := h.svc.GetPostDetail(c, userID, postID)
	if err != nil {
		if err == domain.ErrPostNotFound {
			httputil.NotFound(c, "Post not found")
			return
		}
		httputil.InternalError(c, "Failed to get post")
		return
	}
	httputil.Success(c, vo)
}

// GetPostsRequest 帖子列表请求。
type GetPostsRequest struct {
	Keyword     string `query:"keyword"`
	CircleID    string `query:"circle_id" binding:"omitempty,uuid"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetPosts GET /post/list
func (h *Handler) GetPosts(c appctx.AppContext) {
	var req GetPostsRequest
	if err := c.BindQuery(&req); err != nil {
		logger.Log.Error("Invalid request parameters: " + err.Error())
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	circleID := uuid.Nil
	if req.CircleID != "" {
		var err error
		circleID, err = uuid.Parse(req.CircleID)
		if err != nil {
			httputil.BadRequest(c, "Invalid circle_id")
			return
		}
	}

	size := normalizeSize(req.Size)
	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.SearchPosts(c, req.Keyword, circleID, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search posts: " + err.Error())
		httputil.InternalError(c, "Failed to search posts")
		return
	}
	httputil.Success(c, result)
}

// GetMyPostsRequest 我发布的帖子列表请求。
type GetMyPostsRequest struct {
	Keyword     string `query:"keyword"`
	Size        int    `query:"size"`
	SearchAfter string `query:"search_after"`
}

// GetMyPosts GET /post/my
//
// 查看自己发的帖（按当前登录用户过滤，支持 title/summary 模糊关键字，
// 含草稿/审核等全部状态，仅排除已删除）。
func (h *Handler) GetMyPosts(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetMyPostsRequest
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

	result, err := h.svc.GetMyPosts(c, userID, req.Keyword, size, searchAfter)
	if err != nil {
		logger.Log.Error("Failed to search my posts: " + err.Error())
		httputil.InternalError(c, "Failed to search my posts")
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

// writePostError 把 service 层错误映射到 HTTP 响应。
func writePostError(c appctx.AppContext, err error) {
	switch {
	case application.IsCircleIDRequiredErr(err):
		httputil.BadRequest(c, "circle_id is required")
	case application.IsTitleRequiredErr(err):
		httputil.BadRequest(c, "title is required")
	case application.IsNotMemberErr(err):
		httputil.Forbidden(c, "You are not a member of this circle")
	case application.IsCircleNotAvailableErr(err):
		httputil.Forbidden(c, "This circle is not available for posting")
	case application.IsMembershipPendingErr(err):
		httputil.Forbidden(c, "Your membership is still pending approval")
	case application.IsBannedFromCircleErr(err):
		httputil.Forbidden(c, "You have been banned from this circle")
	default:
		if until, ok := application.IsMutedErr(err); ok {
			httputil.Forbidden(c, "You are muted until "+until.Format("2006-01-02 15:04:05"))
			return
		}
		logger.Log.Error("post service error: " + err.Error())
		httputil.InternalError(c, "Failed to create post")
	}
}
