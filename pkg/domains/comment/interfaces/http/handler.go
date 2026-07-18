// Package http 提供 comment 领域的 HTTP 入站适配器。
package http

import (
	"encoding/json"
	"errors"

	"interestBar/pkg/domains/comment/application"
	"interestBar/pkg/domains/comment/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 comment 相关的 HTTP 请求。
type Handler struct {
	svc application.CommentService
}

// NewHandler 构造 comment Handler。
func NewHandler(svc application.CommentService) *Handler {
	return &Handler{svc: svc}
}

// CreateCommentRequest 发评论/回复的请求结构。
type CreateCommentRequest struct {
	PostID    uuid.UUID       `json:"post_id" binding:"required"`
	Content   string          `json:"content" binding:"required,min=1,max=10000"`
	ExtraData json.RawMessage `json:"extra_data" binding:"omitempty"`
	RootID    *uuid.UUID      `json:"root_id" binding:"omitempty"`
	ReplyToID *uuid.UUID      `json:"reply_to_id" binding:"omitempty"`
}

// CreateComment POST /comment/create
func (h *Handler) CreateComment(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req CreateCommentRequest
	if err := c.BindJSON(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	commentID, err := h.svc.CreateComment(c, userID, application.CreateCommentInput{
		PostID:    req.PostID,
		Content:   req.Content,
		ExtraData: req.ExtraData,
		RootID:    req.RootID,
		ReplyToID: req.ReplyToID,
	})
	if err != nil {
		writeCommentError(c, err)
		return
	}
	httputil.SuccessWithMessage(c, "评论成功", commentID)
}

// GetCommentsRequest 获取顶层评论列表的请求结构。
type GetCommentsRequest struct {
	PostID string `query:"post_id" binding:"required,uuid"`
	Sort   int    `query:"sort" binding:"omitempty,oneof=0 1"` // 0=点赞倒序(默认), 1=时间倒序
	Cursor string `query:"cursor"`
}

// GetComments GET /comment/list
func (h *Handler) GetComments(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetCommentsRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	postID, err := uuid.Parse(req.PostID)
	if err != nil {
		httputil.BadRequest(c, "Invalid post_id")
		return
	}

	result, err := h.svc.GetRootComments(c, userID, postID, req.Sort, req.Cursor)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCursor) {
			httputil.BadRequest(c, "Invalid cursor")
			return
		}
		logger.Log.Error("Failed to get comments: " + err.Error())
		httputil.InternalError(c, "Failed to get comments")
		return
	}
	httputil.Success(c, result)
}

// GetRepliesRequest 获取回复列表的请求结构。
type GetRepliesRequest struct {
	RootID string `query:"root_id" binding:"required,uuid"`
	Sort   int    `query:"sort" binding:"omitempty,oneof=0 1"` // 0=时间倒序(默认), 1=点赞倒序
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit" binding:"omitempty,min=1,max=50"`
}

// GetReplies GET /comment/replies
func (h *Handler) GetReplies(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	var req GetRepliesRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	rootID, err := uuid.Parse(req.RootID)
	if err != nil {
		httputil.BadRequest(c, "Invalid root_id")
		return
	}

	result, err := h.svc.GetReplies(c, userID, rootID, req.Limit, req.Sort, req.Cursor)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCursor) {
			httputil.BadRequest(c, "Invalid cursor")
			return
		}
		if application.IsNotRootCommentErr(err) {
			httputil.BadRequest(c, "Not a root comment")
			return
		}
		if errors.Is(err, domain.ErrCommentNotFound) {
			httputil.NotFound(c, "Root comment not found")
			return
		}
		logger.Log.Error("Failed to get replies: " + err.Error())
		httputil.InternalError(c, "Failed to get replies")
		return
	}
	httputil.Success(c, result)
}

// GetCommentDetail GET /comment/detail/:id
func (h *Handler) GetCommentDetail(c appctx.AppContext) {
	userID, _ := requireUserIDAllowAnon(c)

	commentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.BadRequest(c, "Invalid comment id")
		return
	}

	vo, err := h.svc.GetCommentDetail(c, userID, commentID)
	if err != nil {
		if errors.Is(err, domain.ErrCommentNotFound) {
			httputil.NotFound(c, "Comment not found")
			return
		}
		logger.Log.Error("Failed to get comment: " + err.Error())
		httputil.InternalError(c, "Failed to get comment")
		return
	}
	httputil.Success(c, vo)
}

// ===== 辅助函数 =====

// requireUserID 要求登录并返回 userID。未登录返回 false（已写 401）。
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

// requireUserIDAllowAnon 尝试返回 userID，但允许匿名（未登录返回 uuid.Nil, true）。
//
// 用于"评论列表/详情/回复"这类访客可读接口：登录时回填 is_liked 标记；匿名时 liked=false。
// 路由组挂 OptionalLogin（见 routes.go），故此处匿名是合法路径，不写 401。
func requireUserIDAllowAnon(c appctx.AppContext) (uuid.UUID, bool) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		return uuid.Nil, true
	}
	userID, err := uuid.Parse(loginID)
	if err != nil {
		return uuid.Nil, true
	}
	return userID, true
}

// writeCommentError 把 service 层错误映射到 HTTP 响应。
func writeCommentError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrPostNotFound):
		httputil.NotFound(c, "Post not found")
	case errors.Is(err, domain.ErrPostNotCommentable):
		httputil.Forbidden(c, "Cannot comment on this post")
	case errors.Is(err, domain.ErrPostLocked):
		httputil.Forbidden(c, "This post is locked, comments are not allowed")
	case errors.Is(err, domain.ErrRootCommentNotFound):
		httputil.NotFound(c, "Root comment not found")
	case errors.Is(err, domain.ErrRootCommentMismatch):
		httputil.BadRequest(c, "Root comment does not belong to this post")
	case errors.Is(err, domain.ErrReplyTargetNotFound):
		httputil.NotFound(c, "Reply target comment not found")
	case errors.Is(err, domain.ErrReplyTargetNotInThread):
		httputil.BadRequest(c, "Reply target does not belong to the same thread")
	case errors.Is(err, domain.ErrEmptyContent):
		httputil.BadRequest(c, "Comment content is empty")
	default:
		logger.Log.Error("comment service error: " + err.Error())
		httputil.InternalError(c, "Failed to create comment")
	}
}
