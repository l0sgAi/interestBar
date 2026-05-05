package router

import (
	"interestBar/pkg/logger"
	"interestBar/pkg/server/controller"

	sagin "github.com/click33/sa-token-go/integrations/gin"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.RouterGroup) {
	// Auth routes (公开访问，不需要鉴权)
	auth := r.Group("auth")
	{
		oauthCtrl := controller.NewOAuthController()
		auth.GET("google/login", oauthCtrl.Login("google"))
		auth.GET("google/callback", oauthCtrl.Callback("google"))
		auth.GET("github/login", oauthCtrl.Login("github"))
		auth.GET("github/callback", oauthCtrl.Callback("github"))
		auth.GET("azure/login", oauthCtrl.Login("azure"))
		auth.GET("azure/callback", oauthCtrl.Callback("azure"))

		regCtrl := controller.NewRegisterController()
		auth.POST("register/send-code", regCtrl.SendCode)
		auth.POST("register/verify", regCtrl.VerifyCode)
		auth.POST("register/complete", regCtrl.CompleteRegistration)

		userCtrl := controller.NewUserController()
		// logout 和 me 需要登录
		auth.POST("logout", sagin.CheckLogin(), userCtrl.Logout)
		auth.GET("me", sagin.CheckLogin(), userCtrl.GetCurrentUser)
	}

	// User routes (需要登录)
	userCtrl := controller.NewUserController()
	user := r.Group("user")
	{
		user.GET("get", sagin.CheckLogin(), userCtrl.GetUser)
		user.PUT("update", sagin.CheckLogin(), userCtrl.UpdateProfile)
		user.GET("search", sagin.CheckLogin(), userCtrl.SearchUsers)
		user.GET("detail/:id", sagin.CheckLogin(), userCtrl.GetUserDetail)
	}

	// Upload routes (需要登录鉴权)
	uploadCtrl := controller.NewUploadController(logger.Log)
	upload := r.Group("upload")
	{
		// 上传图片接口 - 使用 sagin.CheckLogin() 进行鉴权
		upload.POST("/image", sagin.CheckLogin(), uploadCtrl.UploadImage)
	}

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

	// Category routes
	categoryCtrl := controller.NewCategoryController()
	category := r.Group("category")
	{
		// 获取分类列表
		category.GET("/get", sagin.CheckLogin(), categoryCtrl.GetCategories)
	}

	// Like routes (需要登录鉴权)
	likeCtrl := controller.NewLikeController()
	like := r.Group("like")
	{
		// 点赞/取消点赞 - 需要登录
		like.POST("/toggle", sagin.CheckLogin(), likeCtrl.ToggleLike)
	}

}
