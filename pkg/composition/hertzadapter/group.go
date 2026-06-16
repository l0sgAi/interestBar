// Package hertzadapter 提供 routing.RouterGroup 的 hertz 实现。
//
// 它把 *route.RouterGroup / *server.Hertz 包装成框架无关的 RouterGroup，
// 让领域包（domains/*/interfaces/http）只依赖 routing 抽象，不 import hertz。
//
// 适配点：hertz 的 handler 是 func(ctx context.Context, c *app.RequestContext)，
// 而抽象要求 func(appctx.AppContext)。这里用闭包做一次包装：先
// hertzadapter.New(ctx, c) 把 RequestContext 转成 AppContext，再调用领域 handler。
//
// 命名说明：本包与 pkg/shared/appctx/hertzadapter 同名但路径不同（本包属于
// composition）。这是有意区分：appctx/hertzadapter 负责"Context 适配"，
// composition/hertzadapter 负责"路由适配"，职责不同。
package hertzadapter

import (
	"context"

	appctxhertz "interestBar/pkg/shared/appctx/hertzadapter"
	"interestBar/pkg/shared/routing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
)

// compile-time guards
var (
	_ routing.RouterGroup = (*engineAdapter)(nil)
	_ routing.RouterGroup = (*groupAdapter)(nil)
)

// ForEngine 把 *server.Hertz 包装成 RouterGroup（根级别）。
func ForEngine(e *server.Hertz) routing.RouterGroup {
	return &engineAdapter{e: e}
}

// ForGroup 把 *route.RouterGroup 包装成 RouterGroup。
func ForGroup(g *route.RouterGroup) routing.RouterGroup {
	return &groupAdapter{g: g}
}

// engineAdapter 适配 *server.Hertz。
type engineAdapter struct {
	e *server.Hertz
}

func (a *engineAdapter) Group(prefix string, mws ...routing.HandlerFunc) routing.RouterGroup {
	handlers := toHertzHandlers(mws)
	return ForGroup(a.e.Group(prefix, handlers...))
}

func (a *engineAdapter) GET(path string, handlers ...routing.HandlerFunc) {
	a.e.GET(path, toHertzHandlers(handlers)...)
}

func (a *engineAdapter) POST(path string, handlers ...routing.HandlerFunc) {
	a.e.POST(path, toHertzHandlers(handlers)...)
}

func (a *engineAdapter) PUT(path string, handlers ...routing.HandlerFunc) {
	a.e.PUT(path, toHertzHandlers(handlers)...)
}

func (a *engineAdapter) DELETE(path string, handlers ...routing.HandlerFunc) {
	a.e.DELETE(path, toHertzHandlers(handlers)...)
}

// groupAdapter 适配 *route.RouterGroup。
type groupAdapter struct {
	g *route.RouterGroup
}

func (a *groupAdapter) Group(prefix string, mws ...routing.HandlerFunc) routing.RouterGroup {
	handlers := toHertzHandlers(mws)
	return ForGroup(a.g.Group(prefix, handlers...))
}

func (a *groupAdapter) GET(path string, handlers ...routing.HandlerFunc) {
	a.g.GET(path, toHertzHandlers(handlers)...)
}

func (a *groupAdapter) POST(path string, handlers ...routing.HandlerFunc) {
	a.g.POST(path, toHertzHandlers(handlers)...)
}

func (a *groupAdapter) PUT(path string, handlers ...routing.HandlerFunc) {
	a.g.PUT(path, toHertzHandlers(handlers)...)
}

func (a *groupAdapter) DELETE(path string, handlers ...routing.HandlerFunc) {
	a.g.DELETE(path, toHertzHandlers(handlers)...)
}

// toHertzHandlers 把 []routing.HandlerFunc 转换为 []app.HandlerFunc。
//
// 关键：在 hertz 调用 handler 前，先把 *app.RequestContext 包成
// appctx.AppContext，再传给领域 handler。这样领域代码拿到的就是框架无关的
// AppContext。
func toHertzHandlers(handlers []routing.HandlerFunc) []app.HandlerFunc {
	result := make([]app.HandlerFunc, 0, len(handlers))
	for _, h := range handlers {
		h := h // 捕获循环变量（Go 1.22+ 默认每轮新变量，这里保险起见显式捕获）
		result = append(result, func(ctx context.Context, c *app.RequestContext) {
			h(appctxhertz.New(ctx, c))
		})
	}
	return result
}
