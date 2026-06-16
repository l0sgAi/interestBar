// Package http 提供 category 领域的 HTTP 入站适配器（handler + 路由注册）。
//
// 这是 category 领域与外部 Web 框架接触的唯一层。
// handler 只做：解析请求 → 调用 application.Service → 用 httputil 写响应。
// 它通过 appctx.AppContext 与具体框架解耦，底层框架（hertz）切换时本文件不动。
package http

import (
	"interestBar/pkg/domains/category/application"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 category 相关的 HTTP 请求。
type Handler struct {
	svc application.CategoryService
}

// NewHandler 构造一个 category Handler。
func NewHandler(svc application.CategoryService) *Handler {
	return &Handler{svc: svc}
}

// GetCategories GET /category/get
//
// 获取分类列表（带缓存）。需登录鉴权（由路由层挂 composition.RequireLogin）。
func (h *Handler) GetCategories(c appctx.AppContext) {
	categories, err := h.svc.GetCategories(c)
	if err != nil {
		httputil.InternalError(c, "Failed to get categories")
		return
	}
	httputil.Success(c, categories)
}
