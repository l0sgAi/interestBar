package model

import (
	"time"

	"gorm.io/gorm"
)

// CommentLike 评论点赞流水表
type CommentLike struct {
	ID         int64      `json:"id" gorm:"primarykey;column:id"`
	UserID     int64      `json:"user_id" gorm:"column:user_id;not null"`             // 点赞人
	CommentID  int64      `json:"comment_id" gorm:"column:comment_id;not null"`       // 被点赞的评论
	PostID     int64      `json:"post_id" gorm:"column:post_id;default:0"`            // 冗余帖子ID
	Deleted    int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"` // 点赞状态: 0=有效点赞, 1=取消点赞
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名
func (CommentLike) TableName() string {
	return "comment_like"
}

// CommentLikeStatus 点赞状态常量
const (
	CommentLikeActive   = 0 // 有效点赞
	CommentLikeCanceled = 1 // 取消点赞
)

// GetCommentLike 获取用户对评论的点赞记录
func GetCommentLike(db *gorm.DB, userID, commentID int64) (*CommentLike, error) {
	var like CommentLike
	err := db.Where("user_id = ? AND comment_id = ?", userID, commentID).First(&like).Error
	if err != nil {
		return nil, err
	}
	return &like, nil
}

// IsCommentLiked 检查用户是否点赞了评论
func IsCommentLiked(db *gorm.DB, userID, commentID int64) (bool, error) {
	var count int64
	err := db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ? AND deleted = ?", userID, commentID, CommentLikeActive).
		Count(&count).Error
	return count > 0, err
}

// GetLikedCommentsByUser 获取用户点赞过的评论列表
func GetLikedCommentsByUser(db *gorm.DB, userID int64, page, pageSize int) ([]CommentLike, int64, error) {
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
func GetCommentLikers(db *gorm.DB, commentID int64, page, pageSize int) ([]CommentLike, int64, error) {
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
func CancelCommentLike(db *gorm.DB, userID, commentID int64) error {
	return db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ?", userID, commentID).
		Update("deleted", CommentLikeCanceled).Error
}

// ReactivateCommentLike 重新激活点赞（取消后再点赞）
func ReactivateCommentLike(db *gorm.DB, userID, commentID int64) error {
	return db.Model(&CommentLike{}).
		Where("user_id = ? AND comment_id = ?", userID, commentID).
		Update("deleted", CommentLikeActive).Error
}
