// Package http 提供 trending 领域的 HTTP 入站适配器。
package http

import (
	"github.com/google/uuid"

	"interestBar/pkg/domains/trending/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 trending 相关的 HTTP 请求。
type Handler struct {
	svc application.TrendingService
}

// NewHandler 构造 trending Handler。
func NewHandler(svc application.TrendingService) *Handler {
	return &Handler{svc: svc}
}

// GetTrendingRequest 热点看板请求（query 绑定，使用 query tag）。
type GetTrendingRequest struct {
	Window  string `query:"window"`  // 24h | 7d（默认 24h）
	Section string `query:"section"` // all | posts | circles | users（默认 all）
	Size    int    `query:"size"`    // 每板块条数，默认 20，上限 50
	Offset  int    `query:"offset"`  // 单板块翻页偏移（section=all 时忽略）
}

// GetTrending GET /trending?window=24h&section=all&size=20
func (h *Handler) GetTrending(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetTrendingRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	result, err := h.svc.GetTrending(c, userID, req.Window, req.Section, req.Size, req.Offset)
	if err != nil {
		logger.Log.Error("trending error: " + err.Error())
		httputil.InternalError(c, "Failed to get trending board")
		return
	}
	httputil.Success(c, result)
}

// requireUserID 解析当前登录用户（匿名 → 401）。热点页强制登录。
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
