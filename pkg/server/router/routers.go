package router

import (
	"interestBar/pkg/server/controller"

	sagin "github.com/click33/sa-token-go/integrations/gin"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册尚未迁移到 domains/ 的领域路由。
//
// 已迁移的领域（category/storage/user/auth）由 composition.RegisterDomainRoutes
// 注册，不在此处出现。待所有领域搬迁完成后，本文件将被删除。
func RegisterRoutes(r *gin.RouterGroup) {
	// Circle routes (需要登录鉴权)
	circleCtrl := controller.NewCircleController()
	circle := r.Group("circle")
	{
		// 创建兴趣圈接口 - 需要登录
		circle.POST("/create", sagin.CheckLogin(), circleCtrl.CreateCircle)
		// 获取圈子列表
		circle.GET("/list", sagin.CheckLogin(), circleCtrl.GetCircles)
		// 获取圈子详情
		circle.GET("/detail/:id", sagin.CheckLogin(), circleCtrl.GetCircleDetail)
		// 获取我加入的圈子列表（支持关键词搜索和分页）
		circle.GET("/my", sagin.CheckLogin(), circleCtrl.GetMyCircles)
		// 加入兴趣圈
		circle.POST("/join", sagin.CheckLogin(), circleCtrl.JoinCircle)
		// 退出兴趣圈
		circle.POST("/leave", sagin.CheckLogin(), circleCtrl.LeaveCircle)
		// 获取圈内帖子列表
		circle.GET("/posts", sagin.CheckLogin(), circleCtrl.GetCirclePosts)
	}

	// Post routes (需要登录鉴权)
	postCtrl := controller.NewPostController()
	post := r.Group("post")
	{
		// 发帖接口 - 需要登录
		post.POST("/create", sagin.CheckLogin(), postCtrl.CreatePost)
		// 获取帖子列表 - 需要登录
		post.GET("/list", sagin.CheckLogin(), postCtrl.GetPosts)
		// 获取帖子详情 - 需要登录
		post.GET("/detail/:id", sagin.CheckLogin(), postCtrl.GetPostDetail)
	}

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

	// Category routes —— 已迁移至 pkg/domains/category，由 composition.RegisterDomainRoutes 注册
	// Storage (upload) routes —— 已迁移至 pkg/domains/storage
	// User routes —— 已迁移至 pkg/domains/user
	// Auth routes —— 已迁移至 pkg/domains/auth

	// Like routes (需要登录鉴权)
	likeCtrl := controller.NewLikeController()
	like := r.Group("like")
	{
		// 点赞/取消点赞 - 需要登录
		like.POST("/toggle", sagin.CheckLogin(), likeCtrl.ToggleLike)
	}
}
