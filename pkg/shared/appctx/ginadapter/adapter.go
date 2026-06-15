// Package ginadapter 提供 AppContext 接口的 gin 实现。
//
// 它把 *gin.Context 包装成 appctx.AppContext，让业务层只依赖 AppContext，
// 不直接 import gin。后续迁移 hertz 时，只需新增一个 hertzadapter，
// 业务代码无需改动。
package ginadapter

import (
	"context"
	"net/http"

	"interestBar/pkg/shared/appctx"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ginAppContext 把 *gin.Context 适配为 appctx.AppContext。
type ginAppContext struct {
	c *gin.Context
	// 内嵌 context：用请求的 ctx 作为标准库 context.Context 的来源
	context.Context
}

// New 把 *gin.Context 包装为 AppContext。
func New(c *gin.Context) appctx.AppContext {
	return &ginAppContext{
		c:       c,
		Context: c.Request.Context(),
	}
}

// ---- 请求信息 ----

func (g *ginAppContext) Method() string       { return g.c.Request.Method }
func (g *ginAppContext) Path() string         { return g.c.Request.URL.Path }
func (g *ginAppContext) Param(name string) string { return g.c.Param(name) }
func (g *ginAppContext) Query(name string) string { return g.c.Query(name) }
func (g *ginAppContext) Header(name string) string { return g.c.GetHeader(name) }
func (g *ginAppContext) PostForm(name string) string { return g.c.PostForm(name) }

// ---- 请求体绑定 ----

func (g *ginAppContext) BindJSON(v any) error {
	return g.c.ShouldBindJSON(v)
}

func (g *ginAppContext) BindQuery(v any) error {
	return g.c.ShouldBindQuery(v)
}

// ---- 响应 ----

func (g *ginAppContext) JSON(code int, v any) {
	g.c.JSON(code, v)
}

func (g *ginAppContext) SetHeader(key, value string) {
	g.c.Header(key, value)
}

// Abort 终止后续中间件执行，映射到 gin.Context.Abort()。
func (g *ginAppContext) Abort() {
	g.c.Abort()
}

// ---- 业务上下文 ----

func (g *ginAppContext) UserID() (uuid.UUID, bool) {
	if v, ok := g.c.Get(userIDKey); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

func (g *ginAppContext) SetUserID(id uuid.UUID) {
	g.c.Set(userIDKey, id)
}

func (g *ginAppContext) LoginID() (string, bool) {
	if v, ok := g.c.Get(loginIDKey); ok {
		if id, ok := v.(string); ok && id != "" {
			return id, true
		}
	}
	return "", false
}

func (g *ginAppContext) SetLoginID(id string) {
	g.c.Set(loginIDKey, id)
}

func (g *ginAppContext) Device() string {
	if v, ok := g.c.Get(deviceKey); ok {
		if d, ok := v.(string); ok {
			return d
		}
	}
	return ""
}

func (g *ginAppContext) SetDevice(device string) {
	g.c.Set(deviceKey, device)
}

// GinContext 返回底层 *gin.Context（过渡期方法，仅供 composition 层的
// 鉴权中间件适配器使用）。
func (g *ginAppContext) GinContext() any {
	return g.c
}

// AsGinContext 是一个便捷函数：如果 AppContext 底层是 gin，返回其
// *gin.Context；否则返回 nil。供 composition 层使用。
func AsGinContext(c appctx.AppContext) (*gin.Context, bool) {
	type ginProvider interface{ GinContext() any }
	if p, ok := c.(ginProvider); ok {
		if gc, ok := p.GinContext().(*gin.Context); ok {
			return gc, true
		}
	}
	return nil, false
}

// Capabilities 返回当前实现的能力（底层是 gin）。
func Capabilities() appctx.Capabilities {
	return appctx.Capabilities{HasGin: true}
}

// gin context 里存取业务值的 key。
// 用私有类型避免与 gin 的其他 Set/Get 冲突。
const (
	userIDKey  = "appctx:user_id"
	loginIDKey = "appctx:login_id"
	deviceKey  = "appctx:device"
)

// compile-time guard: 确保 ginAppContext 实现了 AppContext 接口。
var _ appctx.AppContext = (*ginAppContext)(nil)

// HTTPStatus 仅为方便旧代码迁移保留的快捷常量（可后续移除）。
var _ = http.StatusOK
