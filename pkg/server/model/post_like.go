package model

import (
	"time"

	"gorm.io/gorm"
)

// PostLike 帖子点赞流水表
type PostLike struct {
	ID         int64      `json:"id" gorm:"primarykey;column:id"`
	UserID     int64      `json:"user_id" gorm:"column:user_id;not null"`                      // 点赞人
	PostID     int64      `json:"post_id" gorm:"column:post_id;default:0;not null"`            // 帖子ID
	Deleted    int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`       // 点赞状态: 0=有效点赞, 1=取消点赞
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名
func (PostLike) TableName() string {
	return "post_like"
}

// PostLikeStatus 点赞状态常量
const (
	PostLikeActive   = 0 // 有效点赞
	PostLikeCanceled = 1 // 取消点赞
)

// GetPostLike 获取用户对帖子的点赞记录
func GetPostLike(db *gorm.DB, userID, postID int64) (*PostLike, error) {
	var like PostLike
	err := db.Where("user_id = ? AND post_id = ?", userID, postID).First(&like).Error
	if err != nil {
		return nil, err
	}
	return &like, nil
}

// IsPostLiked 检查用户是否点赞了帖子
func IsPostLiked(db *gorm.DB, userID, postID int64) (bool, error) {
	var count int64
	err := db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, PostLikeActive).
		Count(&count).Error
	return count > 0, err
}

// GetLikedPostsByUser 获取用户点赞过的帖子列表
func GetLikedPostsByUser(db *gorm.DB, userID int64, page, pageSize int) ([]PostLike, int64, error) {
	var likes []PostLike
	var total int64

	query := db.Model(&PostLike{}).Where("user_id = ? AND deleted = ?", userID, PostLikeActive)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&likes).Error

	return likes, total, err
}

// GetPostLikers 获取帖子的点赞者列表
func GetPostLikers(db *gorm.DB, postID int64, page, pageSize int) ([]PostLike, int64, error) {
	var likes []PostLike
	var total int64

	query := db.Model(&PostLike{}).Where("post_id = ? AND deleted = ?", postID, PostLikeActive)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&likes).Error

	return likes, total, err
}

// CreatePostLike 创建点赞记录
func CreatePostLike(db *gorm.DB, like *PostLike) error {
	return db.Create(like).Error
}

// CancelPostLike 取消点赞
func CancelPostLike(db *gorm.DB, userID, postID int64) error {
	return db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Update("deleted", PostLikeCanceled).Error
}

// ReactivatePostLike 重新激活点赞（取消后再点赞）
func ReactivatePostLike(db *gorm.DB, userID, postID int64) error {
	return db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Update("deleted", PostLikeActive).Error
}
