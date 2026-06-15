package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CommentLike 评论点赞流水表
type CommentLike struct {
	BaseModel
	UserID    uuid.UUID  `json:"user_id" gorm:"column:user_id;type:uuid;not null"`       // 点赞人
	CommentID uuid.UUID  `json:"comment_id" gorm:"column:comment_id;type:uuid;not null"` // 被点赞的评论
	PostID    *uuid.UUID `json:"post_id,omitempty" gorm:"column:post_id;type:uuid"`      // 冗余帖子ID，NULL表示仅评论点赞未冗余
	Deleted   int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`  // 点赞状态: 0=有效点赞, 1=取消点赞
}

// TableName 指定表名
func (CommentLike) TableName() string {
	return "domains.comment_like"
}

// CommentLikeStatus 点赞状态常量
const (
	CommentLikeActive   = 0 // 有效点赞
	CommentLikeCanceled = 1 // 取消点赞
)

// GetCommentLike 获取用户对评论的点赞记录
func GetCommentLike(db *gorm.DB, userID, commentID uuid.UUID) (*CommentLike, error) {
	var like CommentLike
	err := db.Where("user_id = ? AND comment_id = ?", userID, commentID).First(&like).Error
	if err != nil {
		return nil, err
	}
	return &like, nil
}

// IsCommentLiked 检查用户是否点赞了评论
func IsCommentLiked(db *gorm.DB, userID, commentID uuid.UUID) (bool, error) {
	var count int64
	err := db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ? AND deleted = ?", userID, commentID, CommentLikeActive).
		Count(&count).Error
	return count > 0, err
}

// GetLikedCommentsByUser 获取用户点赞过的评论列表
func GetLikedCommentsByUser(db *gorm.DB, userID uuid.UUID, page, pageSize int) ([]CommentLike, int64, error) {
	var likes []CommentLike
	var total int64

	query := db.Model(&CommentLike{}).Where("user_id = ? AND deleted = ?", userID, CommentLikeActive)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&likes).Error

	return likes, total, err
}

// GetCommentLikers 获取评论的点赞者列表
func GetCommentLikers(db *gorm.DB, commentID uuid.UUID, page, pageSize int) ([]CommentLike, int64, error) {
	var likes []CommentLike
	var total int64

	query := db.Model(&CommentLike{}).Where("comment_id = ? AND deleted = ?", commentID, CommentLikeActive)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&likes).Error

	return likes, total, err
}

// CreateCommentLike 创建点赞记录
func CreateCommentLike(db *gorm.DB, like *CommentLike) error {
	return db.Create(like).Error
}

// CancelCommentLike 取消点赞
func CancelCommentLike(db *gorm.DB, userID, commentID uuid.UUID) error {
	return db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ?", userID, commentID).
		Update("deleted", CommentLikeCanceled).Error
}

// ReactivateCommentLike 重新激活点赞（取消后再点赞）
func ReactivateCommentLike(db *gorm.DB, userID, commentID uuid.UUID) error {
	return db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ?", userID, commentID).
		Update("deleted", CommentLikeActive).Error
}

// BatchCheckCommentLiked 批量检查用户是否点赞了多条评论
func BatchCheckCommentLiked(db *gorm.DB, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool), nil
	}
	var likes []CommentLike
	err := db.Where("user_id = ? AND comment_id IN ? AND deleted = ?", userID, commentIDs, CommentLikeActive).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(likes))
	for _, like := range likes {
		result[like.CommentID] = true
	}
	return result, nil
}
