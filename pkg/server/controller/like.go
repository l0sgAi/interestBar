package controller

import (
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/db/pgsql"
	redpanda "interestBar/pkg/server/storage/redpanda"
	redispkg "interestBar/pkg/server/storage/redis"
	"interestBar/pkg/server/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LikeController 处理点赞相关操作
type LikeController struct{}

// NewLikeController 创建点赞控制器
func NewLikeController() *LikeController {
	return &LikeController{}
}

// ToggleLikeRequest 点赞/取消点赞请求
type ToggleLikeRequest struct {
	Type     string `json:"type" binding:"required,oneof=comment post"` // "comment" 或 "post"
	TargetID int64  `json:"target_id" binding:"required,min=1"`
}

// ToggleLike 点赞/取消点赞（幂等操作）
// POST /like/toggle
func (ctrl *LikeController) ToggleLike(c *gin.Context) {
	userID, ok := utils.GetUserIDFromRequest(c)
	if !ok {
		return
	}

	var req ToggleLikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request parameters")
		return
	}

	var result redispkg.ToggleLikeResult
	var err error
	var postID int64

	switch req.Type {
	case "comment":
		comment, err := model.GetCommentByID(pgsql.DB, req.TargetID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				response.NotFound(c, "Comment not found")
				return
			}
			response.InternalError(c, "Failed to check comment")
			return
		}
		postID = comment.PostID

		// 确保评论统计缓存存在
		if err := restoreCommentStatsIfNeed(req.TargetID); err != nil {
			logger.Log.Error("Failed to restore comment stats cache: " + err.Error())
		}

		result, err = redispkg.ToggleCommentLike(int64(userID), req.TargetID)

	case "post":
		_, err := model.GetPostByID(pgsql.DB, req.TargetID)
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				response.NotFound(c, "Post not found")
				return
			}
			response.InternalError(c, "Failed to check post")
			return
		}

		// 确保帖子统计缓存存在
		if err := restorePostStatsIfNeed(req.TargetID); err != nil {
			logger.Log.Error("Failed to restore post stats cache: " + err.Error())
		}

		result, err = redispkg.TogglePostLike(int64(userID), req.TargetID)
	}

	if err != nil {
		response.InternalError(c, "Failed to toggle like")
		return
	}

	// 发送MQ消息用于持久化到数据库
	amount := int64(result)
	switch req.Type {
	case "comment":
		if err := redpanda.PublishCommentLikeEvent(int64(userID), req.TargetID, postID, amount); err != nil {
			logger.Log.Error("Failed to publish comment like event: " + err.Error())
		}
	case "post":
		if err := redpanda.PublishPostLikeEvent(int64(userID), req.TargetID, amount); err != nil {
			logger.Log.Error("Failed to publish post like event: " + err.Error())
		}
	}

	response.Success(c, map[string]interface{}{
		"is_liked":  result == redispkg.ToggleLikeLiked,
		"type":      req.Type,
		"target_id": req.TargetID,
	})
}

// restoreCommentStatsIfNeed 恢复评论统计信息到Redis缓存（如果缓存不存在）
func restoreCommentStatsIfNeed(commentID int64) error {
	exists, err := redispkg.CommentStatisticsExists(commentID)
	if err != nil || exists {
		return err
	}
	comment, err := model.GetCommentByID(pgsql.DB, commentID)
	if err != nil {
		return err
	}
	return redispkg.UpdateCommentStatistics(commentID, comment.LikeCount)
}
