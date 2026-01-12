package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Post 帖子主表
type Post struct {
	ID            int64          `json:"id" gorm:"primarykey;column:id"`
	CircleID      int64          `json:"circle_id" gorm:"column:circle_id;not null"`                      // 所属圈子ID
	UserID        int64          `json:"user_id" gorm:"column:user_id;not null"`                          // 发帖人ID
	Type          int16          `json:"type" gorm:"column:type;type:smallint;default:1"`                 // 帖子类型
	Title         string         `json:"title" gorm:"column:title;type:varchar(200);default:''"`          // 标题
	Summary       string         `json:"summary" gorm:"column:summary;type:varchar(500);default:''"`      // 摘要
	Content       string         `json:"content" gorm:"column:content;type:text;default:''"`              // 正文
	MediaExtra    MediaExtraJSON `json:"media_extra" gorm:"column:media_extra;type:jsonb;default:'{}'::jsonb"` // 媒体扩展信息
	ViewCount     int            `json:"view_count" gorm:"column:view_count;default:0"`                   // 浏览量
	CommentCount  int            `json:"comment_count" gorm:"column:comment_count;default:0"`             // 评论数
	LikeCount     int            `json:"like_count" gorm:"column:like_count;default:0"`                   // 点赞数
	CollectCount  int            `json:"collect_count" gorm:"column:collect_count;default:0"`             // 收藏数
	IsPinned      int16          `json:"is_pinned" gorm:"column:is_pinned;type:smallint;default:0"`       // 是否置顶
	IsEssence     int16          `json:"is_essence" gorm:"column:is_essence;type:smallint;default:0"`     // 是否加精
	IsLock        int16          `json:"is_lock" gorm:"column:is_lock;type:smallint;default:0"`           // 是否锁定
	Status        int16          `json:"status" gorm:"column:status;type:smallint;default:1"`             // 状态
	Deleted       int16          `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`           // 逻辑删除
	CreateTime    time.Time      `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime    time.Time      `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	LastReplyTime *time.Time     `json:"last_reply_time,omitempty" gorm:"column:last_reply_time"`         // 最后回复时间
}

// TableName 指定表名
func (Post) TableName() string {
	return "post"
}

// PostType 帖子类型常量
const (
	PostTypeTextImage = 1 // 图文
	PostTypeVideo     = 2 // 纯视频
	PostTypeVote      = 3 // 投票/链接
)

// PostStatus 帖子状态常量
const (
	PostStatusDraft      = 0 // 草稿
	PostStatusPublished  = 1 // 发布(正常)
	PostStatusReviewing  = 2 // 审核中
	PostStatusRejected   = 3 // 审核失败
	PostStatusBlocked    = 4 // 被屏蔽(软删/违规)
)

// MediaExtraJSON 媒体扩展信息JSON类型
type MediaExtraJSON map[string]interface{}

// Scan 实现 sql.Scanner 接口
func (m *MediaExtraJSON) Scan(value interface{}) error {
	if value == nil {
		*m = make(MediaExtraJSON)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, m)
}

// Value 实现 driver.Valuer 接口
func (m MediaExtraJSON) Value() (driver.Value, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// GetPostByID 根据ID获取帖子
func GetPostByID(db *gorm.DB, postID int64) (*Post, error) {
	var post Post
	err := db.Where("id = ? AND deleted = ?", postID, 0).First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

// GetPostsByCircle 获取圈子下的帖子列表
func GetPostsByCircle(db *gorm.DB, circleID int64, page, pageSize int) ([]Post, int64, error) {
	var posts []Post
	var total int64

	query := db.Model(&Post{}).Where("circle_id = ? AND status = ? AND deleted = ?", circleID, PostStatusPublished, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询 (置顶帖在前，然后按创建时间倒序)
	err := db.Where("circle_id = ? AND is_pinned = ? AND status = ? AND deleted = ?", circleID, 1, PostStatusPublished, 0).
		Order("create_time DESC").
		Find(&posts).Error

	if err != nil {
		return nil, 0, err
	}

	// 查询非置顶帖子
	var normalPosts []Post
	err = db.Where("circle_id = ? AND is_pinned = ? AND status = ? AND deleted = ?", circleID, 0, PostStatusPublished, 0).
		Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&normalPosts).Error

	if err != nil {
		return nil, 0, err
	}

	// 合并置顶帖和普通帖子
	posts = append(posts, normalPosts...)

	return posts, total, err
}

// GetPinnedPostsByCircle 获取圈子置顶帖子
func GetPinnedPostsByCircle(db *gorm.DB, circleID int64) ([]Post, error) {
	var posts []Post
	err := db.Where("circle_id = ? AND is_pinned = ? AND deleted = ?", circleID, 1, 0).
		Order("create_time DESC").
		Find(&posts).Error
	return posts, err
}

// GetPostsByUser 获取用户的帖子列表
func GetPostsByUser(db *gorm.DB, userID int64, page, pageSize int) ([]Post, int64, error) {
	var posts []Post
	var total int64

	query := db.Model(&Post{}).Where("user_id = ? AND deleted = ?", userID, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&posts).Error

	return posts, total, err
}

// GetPostsByStatus 根据状态获取帖子列表（用于后台审核）
func GetPostsByStatus(db *gorm.DB, status int16, page, pageSize int) ([]Post, int64, error) {
	var posts []Post
	var total int64

	query := db.Model(&Post{}).Where("status = ? AND deleted = ?", status, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&posts).Error

	return posts, total, err
}

// IncrementViewCount 增加浏览量
func IncrementViewCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("view_count", gorm.Expr("view_count + ?", 1)).Error
}

// IncrementCommentCount 增加评论数
func IncrementCommentCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("comment_count", gorm.Expr("comment_count + ?", 1)).Error
}

// DecrementCommentCount 减少评论数
func DecrementCommentCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("comment_count", gorm.Expr("comment_count - ?", 1)).Error
}

// IncrementLikeCount 增加点赞数
func IncrementLikeCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("like_count", gorm.Expr("like_count + ?", 1)).Error
}

// DecrementLikeCount 减少点赞数
func DecrementLikeCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("like_count", gorm.Expr("like_count - ?", 1)).Error
}

// CreatePost 创建帖子（包含权限校验）
func CreatePost(db *gorm.DB, post *Post) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. 插入帖子
		if err := tx.Create(post).Error; err != nil {
			return err
		}

		// 2. 更新圈子的帖子计数
		if err := tx.Model(&Circle{}).Where("id = ?", post.CircleID).
			UpdateColumn("post_count", gorm.Expr("post_count + ?", 1)).Error; err != nil {
			return err
		}

		return nil
	})
}
