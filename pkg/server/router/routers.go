package router

import (
	"interestBar/pkg/server/controller"

	sagin "github.com/click33/sa-token-go/integrations/gin"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册尚未迁移到 domains/ 的领域路由。
//
// 已迁移的领域（category/storage/user/auth/circle/post）由
// composition.RegisterDomainRoutes 注册。待所有领域搬迁完成后，
// 本文件将被删除。
func RegisterRoutes(r *gin.RouterGroup) {
	// Comment routes (需要登录鉴权)
	commentCtrl := controller.NewCommentController()
	comment := r.Group("comment")
	{
		// 发评论/回复 - 需要登录
		comment.POST("/create", sagin.CheckLogin(), commentCtrl.CreateComment)
		// 获取评论列表 - 需要登录
		comment.GET("/list", sagin.CheckLogin(), commentCtrl.GetComments)
		// 获取评论的子回复列表 - 需要登录
		comment.GET("/replies", sagin.CheckLogin(), commentCtrl.GetReplies)
		// 获取评论详情 - 需要登录
		comment.GET("/detail/:id", sagin.CheckLogin(), commentCtrl.GetCommentDetail)
	}

	// Category routes —— 已迁移至 pkg/domains/category
	// Storage (upload) routes —— 已迁移至 pkg/domains/storage
	// User routes —— 已迁移至 pkg/domains/user
	// Auth routes —— 已迁移至 pkg/domains/auth
	// Circle routes —— 已迁移至 pkg/domains/circle
	// Post routes —— 已迁移至 pkg/domains/post

	// Like routes (需要登录鉴权)
	likeCtrl := controller.NewLikeController()
	like := r.Group("like")
	{
		// 点赞/取消点赞 - 需要登录
		like.POST("/toggle", sagin.CheckLogin(), likeCtrl.ToggleLike)
	}
}
