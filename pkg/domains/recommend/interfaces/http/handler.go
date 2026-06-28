// Package http 提供 recommend 领域的 HTTP 入站适配器。
package http

import (
	"encoding/json"
	"errors"

	"interestBar/pkg/domains/recommend/application"
	"interestBar/pkg/logger"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"

	"github.com/google/uuid"
)

// Handler 处理 recommend 相关的 HTTP 请求。
type Handler struct {
	svc application.RecommendService
}

// NewHandler 构造 recommend Handler。
func NewHandler(svc application.RecommendService) *Handler {
	return &Handler{svc: svc}
}

// GetHomeFeedRequest 首页信息流请求。
type GetHomeFeedRequest struct {
	Tab         string `query:"tab"`          // recommend | hot | latest | following
	Size        int    `query:"size"`         // 每页数量，默认 20
	Offset      int    `query:"offset"`       // recommend：候选池偏移
	PoolToken   string `query:"pool_token"`   // recommend：上次返回的池版本 token
	SearchAfter string `query:"search_after"` // hot/latest/following：ES search_after 游标
}

// GetHomeFeed GET /post/home?tab=recommend
func (h *Handler) GetHomeFeed(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	var req GetHomeFeedRequest
	if err := c.BindQuery(&req); err != nil {
		httputil.BadRequest(c, "Invalid request parameters")
		return
	}

	searchAfter, ok := parseSearchAfter(c, req.SearchAfter)
	if !ok {
		return
	}

	result, err := h.svc.GetHomeFeed(c, userID, req.Tab, req.Size, req.Offset, req.PoolToken, searchAfter)
	if err != nil {
		if errors.Is(err, application.ErrTabNotSupported) {
			httputil.BadRequest(c, "Unsupported home feed tab")
			return
		}
		logger.Log.Error("home feed error: " + err.Error())
		httputil.InternalError(c, "Failed to get home feed")
		return
	}
	httputil.Success(c, result)
}

// requireUserID 解析当前登录用户（匿名 → 401）。推荐流强制登录。
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

// parseSearchAfter 解析 search_after 游标（JSON 数组）。空串=首页；非法→400。
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
