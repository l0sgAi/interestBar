// Package routing 提供领域路由注册的框架无关抽象。
//
// 设计目的：让每个领域包自己注册路由（routes.go），但又不直接依赖
// hertz，这样未来拆分微服务时，某领域包可以整体搬走而不带
// 框架包袱。
//
// 使用方式：
//  1. composition 层把 hertz engine 包装成 RouterGroup 接口；
//  2. 调用 domains/<x>/interfaces/http.RegisterRoutes(rg, deps...)。
package routing

import (
	"interestBar/pkg/shared/appctx"
)

// HandlerFunc 是框架无关的请求处理函数签名。
//
// 接收 appctx.AppContext，不暴露任何 hertz 类型。
// composition 层负责把 *app.RequestContext 包装成 appctx.AppContext 后再调用。
type HandlerFunc func(c appctx.AppContext)

// RouterGroup 是路由组的抽象接口。
//
// 各领域的 RegisterRoutes 接收此接口，完成路由挂载。
// composition 层提供 hertz 版实现。
type RouterGroup interface {
	// Group 创建子路由组，prefix 是路径前缀（如 "/category"）。
	Group(prefix string, middlewares ...HandlerFunc) RouterGroup

	// GET/POST/PUT/DELETE 注册路由。
	GET(relativePath string, handlers ...HandlerFunc)
	POST(relativePath string, handlers ...HandlerFunc)
	PUT(relativePath string, handlers ...HandlerFunc)
	DELETE(relativePath string, handlers ...HandlerFunc)
}
