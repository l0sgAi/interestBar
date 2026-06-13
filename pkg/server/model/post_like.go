package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PostLike 帖子点赞流水表
type PostLike struct {
	BaseModel
	UserID  uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`      // 点赞人
	PostID  uuid.UUID `json:"post_id" gorm:"column:post_id;type:uuid;not null"`      // 帖子ID (必填)
	Deleted int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"` // 点赞状态: 0=有效点赞, 1=取消点赞
}

// TableName 指定表名
func (PostLike) TableName() string {
	return "domains.post_like"
}

// PostLikeStatus 点赞状态常量
const (
	PostLikeActive   = 0 // 有效点赞
	PostLikeCanceled = 1 // 取消点赞
)

// GetPostLike 获取用户对帖子的点赞记录
func GetPostLike(db *gorm.DB, userID, postID uuid.UUID) (*PostLike, error) {
	var like PostLike
	err := db.Where("user_id = ? AND post_id = ?", userID, postID).First(&like).Error
	if err != nil {
		return nil, err
	}
	return &like, nil
}

// IsPostLiked 检查用户是否点赞了帖子
func IsPostLiked(db *gorm.DB, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, PostLikeActive).
		Count(&count).Error
	return count > 0, err
}

// GetLikedPostsByUser 获取用户点赞过的帖子列表
func GetLikedPostsByUser(db *gorm.DB, userID uuid.UUID, page, pageSize int) ([]PostLike, int64, error) {
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
func GetPostLikers(db *gorm.DB, postID uuid.UUID, page, pageSize int) ([]PostLike, int64, error) {
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
func CancelPostLike(db *gorm.DB, userID, postID uuid.UUID) error {
	return db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Update("deleted", PostLikeCanceled).Error
}

// ReactivatePostLike 重新激活点赞（取消后再点赞）
func ReactivatePostLike(db *gorm.DB, userID, postID uuid.UUID) error {
	return db.Model(&PostLike{}).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Update("deleted", PostLikeActive).Error
}

// BatchCheckPostLiked 批量检查用户是否点赞了多个帖子
func BatchCheckPostLiked(db *gorm.DB, userID uuid.UUID, postIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]bool), nil
	}
	var likes []PostLike
	err := db.Where("user_id = ? AND post_id IN ? AND deleted = ?", userID, postIDs, PostLikeActive).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]bool, len(likes))
	for _, like := range likes {
		result[like.PostID] = true
	}
	return result, nil
}
