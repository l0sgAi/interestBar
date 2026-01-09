package model

import (
	"time"

	"gorm.io/gorm"
)

// Comment 评论表
type Comment struct {
	ID          int64      `json:"id" gorm:"primarykey;column:id"`
	PostID      int64      `json:"post_id" gorm:"column:post_id;not null"`                // 所属帖子ID
	UserID      int64      `json:"user_id" gorm:"column:user_id;not null"`                // 评论发布者ID
	RootID      int64      `json:"root_id" gorm:"column:root_id;default:0"`                // 根评论ID，0为根评论
	ReplyToID   int64      `json:"reply_to_id" gorm:"column:reply_to_id;default:0"`        // 被回复的评论ID，0为非回复
	Content     string     `json:"content" gorm:"column:content;type:text;not null"`       // 评论内容
	LikeCount   int        `json:"like_count" gorm:"column:like_count;default:0"`          // 点赞数
	ReplyCount  int        `json:"reply_count" gorm:"column:reply_count;default:0"`        // 子评论数
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`    // 状态
	Deleted     int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`  // 逻辑删除
	CreateTime  time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名
func (Comment) TableName() string {
	return "comment"
}

// CommentStatus 评论状态常量
const (
	CommentStatusNormal   = 1 // 正常
	CommentStatusReview   = 2 // 审核中
	CommentStatusHidden   = 3 // 折叠/隐藏
)

// GetCommentByID 根据ID获取评论
func GetCommentByID(db *gorm.DB, commentID int64) (*Comment, error) {
	var comment Comment
	err := db.Where("id = ? AND deleted = ?", commentID, 0).First(&comment).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

// GetRootCommentsByPost 获取帖子的顶级评论列表
func GetRootCommentsByPost(db *gorm.DB, postID int64, page, pageSize int) ([]Comment, int64, error) {
	var comments []Comment
	var total int64

	query := db.Model(&Comment{}).Where("post_id = ? AND root_id = ? AND deleted = ?", postID, 0, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询，按点赞数和时间倒序
	err := query.Order("like_count DESC, create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error

	return comments, total, err
}

// GetSubCommentsByRoot 获取某条评论的子回复列表
func GetSubCommentsByRoot(db *gorm.DB, rootID int64, page, pageSize int) ([]Comment, int64, error) {
	var comments []Comment
	var total int64

	query := db.Model(&Comment{}).Where("root_id = ? AND deleted = ?", rootID, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询，按时间正序
	err := query.Order("create_time ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error

	return comments, total, err
}

// GetCommentsByUser 获取用户的评论历史
func GetCommentsByUser(db *gorm.DB, userID int64, page, pageSize int) ([]Comment, int64, error) {
	var comments []Comment
	var total int64

	query := db.Model(&Comment{}).Where("user_id = ? AND deleted = ?", userID, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error

	return comments, total, err
}

// IncrementLikeCount 增加点赞数
func IncrementCommentLikeCount(db *gorm.DB, commentID int64) error {
	return db.Model(&Comment{}).Where("id = ?", commentID).
		UpdateColumn("like_count", gorm.Expr("like_count + ?", 1)).Error
}

// DecrementLikeCount 减少点赞数
func DecrementCommentLikeCount(db *gorm.DB, commentID int64) error {
	return db.Model(&Comment{}).Where("id = ?", commentID).
		UpdateColumn("like_count", gorm.Expr("like_count - ?", 1)).Error
}

// IncrementReplyCount 增加回复数
func IncrementReplyCount(db *gorm.DB, rootID int64) error {
	return db.Model(&Comment{}).Where("id = ?", rootID).
		UpdateColumn("reply_count", gorm.Expr("reply_count + ?", 1)).Error
}

// DecrementReplyCount 减少回复数
func DecrementReplyCount(db *gorm.DB, rootID int64) error {
	return db.Model(&Comment{}).Where("id = ?", rootID).
		UpdateColumn("reply_count", gorm.Expr("reply_count - ?", 1)).Error
}
