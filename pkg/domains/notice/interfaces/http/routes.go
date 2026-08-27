package http

import (
	"interestBar/pkg/domains/notice/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 notice 领域的路由挂到给定的路由组根上。
//
// 路由清单：
//
//	GET  /notice/list          通知列表（需登录，keyset 分页）
//	GET  /notice/unread-count  未读数（需登录）
//	POST /notice/read          批量已读（需登录）
//	POST /notice/read-all      全部已读（需登录）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.NoticeService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	g := rg.Group("/notice", authCheck)
	{
		g.GET("/list", h.ListNotices)
		g.GET("/unread-count", h.GetUnreadCount)
		g.POST("/read", h.MarkRead)
		g.POST("/read-all", h.MarkAllRead)
	}
}
