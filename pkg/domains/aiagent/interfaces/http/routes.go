package http

import (
	"interestBar/pkg/domains/aiagent/application"
	"interestBar/pkg/shared/routing"
)

// RegisterRoutes 把 aiagent 领域的路由挂到给定的路由组根上。
//
// 路由清单（全部需登录）：
//
// 全局机器人（role=1 管理员校验在 service 层，非管理员返回 403）：
//
//	POST   /agent                   创建机器人
//	GET    /agent/list              机器人列表（offset 分页）
//	GET    /agent/:id               机器人详情
//	PUT    /agent/:id               更新机器人（部分更新）
//	DELETE /agent/:id               软删机器人（停用）
//	POST   /agent/:id/reply/:postId 手动触发机器人回复（trigger_mode=3）
//
// 圈子级机器人（圈内 admin+ 校验在 service 层；凭据字段/删除/手动触发回复仅圈主）：
//
//	POST   /circle/agent                  创建圈内机器人（admin+，每圈 ≤5）
//	GET    /circle/agent/list             圈内机器人列表（admin+；query: circle_id, keyword, page, size）
//	GET    /circle/agent/:id              圈内机器人详情（该圈 admin+）
//	PUT    /circle/agent/:id              更新（字段分级：凭据字段仅圈主）
//	DELETE /circle/agent/:id              软删（仅圈主）
//	POST   /circle/agent/:id/reply/:postId 手动触发回复（仅圈主；trigger_mode=3；
//	                                        帖子必须属于机器人所在圈）
//
// 两条链路跨作用域互不可见（service 层守卫，错误统一 404）。
func RegisterRoutes(
	rg routing.RouterGroup,
	svc application.AgentService,
	circleAgentSvc application.CircleAgentService,
	replySvc application.ReplyService,
	authCheck routing.HandlerFunc,
) {
	h := NewHandler(svc, replySvc)
	ch := NewCircleAgentHandler(circleAgentSvc, replySvc)

	g := rg.Group("/agent", authCheck)
	{
		g.POST("", h.CreateAgent)
		g.GET("/list", h.ListAgents)
		g.GET("/:id", h.GetAgent)
		g.PUT("/:id", h.UpdateAgent)
		g.DELETE("/:id", h.DeleteAgent)
		g.POST("/:id/reply/:postId", h.ManualReply)
	}

	cg := rg.Group("/circle/agent", authCheck)
	{
		cg.POST("", ch.CreateCircleAgent)
		cg.GET("/list", ch.ListCircleAgents)
		cg.GET("/:id", ch.GetCircleAgent)
		cg.PUT("/:id", ch.UpdateCircleAgent)
		cg.DELETE("/:id", ch.DeleteCircleAgent)
		cg.POST("/:id/reply/:postId", ch.CircleManualReply)
	}
}
