// Package ginadapter 提供 routing.RouterGroup 的 gin 实现。
//
// 它把 *gin.RouterGroup / *gin.Engine 包装成框架无关的 RouterGroup，
// 让领域包（domains/*/interfaces/http）只依赖 routing 抽象，不 import gin。
//
// 适配点：gin 的 handler 是 func(*gin.Context)，而抽象要求
// func(appctx.AppContext)。这里用闭包做一次包装：先 ginadapter.New(c)
// 把 *gin.Context 转成 AppContext，再调用领域 handler。
//
// 命名说明：本包与 pkg/shared/appctx/ginadapter 同名但路径不同（本包属于
// composition）。这是有意区分：appctx/ginadapter 负责"Context 适配"，
// composition/ginadapter 负责"路由适配"，职责不同。
package ginadapter

import (
	appctxgin "interestBar/pkg/shared/appctx/ginadapter"
	"interestBar/pkg/shared/routing"

	"github.com/gin-gonic/gin"
)

// compile-time guards
var (
	_ routing.RouterGroup = (*engineAdapter)(nil)
	_ routing.RouterGroup = (*groupAdapter)(nil)
)

// ForEngine 把 *gin.Engine 包装成 RouterGroup（根级别）。
func ForEngine(e *gin.Engine) routing.RouterGroup {
	return &engineAdapter{e: e}
}

// ForGroup 把 *gin.RouterGroup 包装成 RouterGroup。
func ForGroup(g *gin.RouterGroup) routing.RouterGroup {
	return &groupAdapter{g: g}
}

// engineAdapter 适配 *gin.Engine。
type engineAdapter struct {
	e *gin.Engine
}

func (a *engineAdapter) Group(prefix string, mws ...routing.HandlerFunc) routing.RouterGroup {
	handlers := toGinHandlers(mws)
	return ForGroup(a.e.Group(prefix, handlers...))
}

func (a *engineAdapter) GET(path string, handlers ...routing.HandlerFunc) {
	a.e.GET(path, toGinHandlers(handlers)...)
}

func (a *engineAdapter) POST(path string, handlers ...routing.HandlerFunc) {
	a.e.POST(path, toGinHandlers(handlers)...)
}

func (a *engineAdapter) PUT(path string, handlers ...routing.HandlerFunc) {
	a.e.PUT(path, toGinHandlers(handlers)...)
}

func (a *engineAdapter) DELETE(path string, handlers ...routing.HandlerFunc) {
	a.e.DELETE(path, toGinHandlers(handlers)...)
}

// groupAdapter 适配 *gin.RouterGroup。
type groupAdapter struct {
	g *gin.RouterGroup
}

func (a *groupAdapter) Group(prefix string, mws ...routing.HandlerFunc) routing.RouterGroup {
	handlers := toGinHandlers(mws)
	return ForGroup(a.g.Group(prefix, handlers...))
}

func (a *groupAdapter) GET(path string, handlers ...routing.HandlerFunc) {
	a.g.GET(path, toGinHandlers(handlers)...)
}

func (a *groupAdapter) POST(path string, handlers ...routing.HandlerFunc) {
	a.g.POST(path, toGinHandlers(handlers)...)
}

func (a *groupAdapter) PUT(path string, handlers ...routing.HandlerFunc) {
	a.g.PUT(path, toGinHandlers(handlers)...)
}

func (a *groupAdapter) DELETE(path string, handlers ...routing.HandlerFunc) {
	a.g.DELETE(path, toGinHandlers(handlers)...)
}

// toGinHandlers 把 []routing.HandlerFunc 转换为 []gin.HandlerFunc。
//
// 关键：在 gin 调用 handler 前，先把 *gin.Context 包成 appctx.AppContext，
// 再传给领域 handler。这样领域代码拿到的就是框架无关的 AppContext。
func toGinHandlers(handlers []routing.HandlerFunc) []gin.HandlerFunc {
	result := make([]gin.HandlerFunc, 0, len(handlers))
	for _, h := range handlers {
		h := h // 捕获循环变量（Go 1.22+ 默认每轮新变量，这里保险起见显式捕获）
		result = append(result, func(c *gin.Context) {
			h(appctxgin.New(c))
		})
	}
	return result
}

