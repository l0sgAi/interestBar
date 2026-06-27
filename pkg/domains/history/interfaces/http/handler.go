// Package http 提供 history 领域的 HTTP 入站适配器。
package http

import (
	"errors"

	"interestBar/pkg/domains/history/application"
	"interestBar/pkg/domains/history/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 history 相关的 HTTP 请求。
type Handler struct {
	svc application.HistoryService
}

// NewHandler 构造 history Handler。
func NewHandler(svc application.HistoryService) *Handler {
	return &Handler{svc: svc}
}

// ListHistoryPostsRequest 「最近浏览」列表请求。
type ListHistoryPostsRequest struct {
	Size   int `query:"size"`
	Offset int `query:"offset"`
}

// ListHistoryPosts GET /history/posts
func (h *Handler) ListHistoryPosts(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req ListHistoryPostsRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.ListHistoryPosts(c, userID, req.Size, req.Offset)
	if err != nil {
		writeHistoryError(c, err)
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

// writeHistoryError 把 service 层错误映射到 HTTP 响应。
func writeHistoryError(c appctx.AppContext, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCursor):
		httputil.BadRequest(c, "Invalid pagination parameter")
	default:
		logger.Log.Error("history service error: " + err.Error())
		httputil.InternalError(c, "Failed to process history request")
	}
}
