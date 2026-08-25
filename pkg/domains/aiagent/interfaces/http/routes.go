package http

import (
	"interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 aiagent 领域的路由挂到给定的路由组根上。
//
// 路由清单（全部需登录；role=1 管理员校验在 service 层，非管理员返回 403）：
//
//	POST   /agent        创建机器人
//	GET    /agent/list   机器人列表（offset 分页）
//	GET    /agent/:id    机器人详情
//	PUT    /agent/:id    更新机器人（部分字段）
//	DELETE /agent/:id    软删机器人（停用）
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.AgentService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc)

	g := rg.Group("/agent", authCheck)
	{
		g.POST("", h.CreateAgent)
		g.GET("/list", h.ListAgents)
		g.GET("/:id", h.GetAgent)
		g.PUT("/:id", h.UpdateAgent)
		g.DELETE("/:id", h.DeleteAgent)
	}
}
