package model

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Comment 评论表
type Comment struct {
	ID             int64      `json:"id" gorm:"primarykey;column:id"`
	PostID         int64      `json:"post_id" gorm:"column:post_id;not null"`                      // 所属帖子ID
	UserID         int64      `json:"user_id" gorm:"column:user_id;not null"`                      // 评论发布者ID
	RootID         int64      `json:"root_id" gorm:"column:root_id;default:0"`                      // 根评论ID，0为根评论
	ReplyToID      int64      `json:"reply_to_id" gorm:"column:reply_to_id;default:0"`              // 被回复的评论ID，0为非回复
	ReplyToUserID  int64      `json:"reply_to_user_id" gorm:"column:reply_to_user_id;default:0"`     // 被回复用户ID，0为非回复
	Content        string     `json:"content" gorm:"column:content;type:text;not null"`             // 评论内容
	ExtraData      json.RawMessage `json:"extra_data" gorm:"column:extra_data;type:jsonb;default:'{}'::jsonb"` // 扩展数据（JSON格式，如图片URL数组等）
	LikeCount      int        `json:"like_count" gorm:"column:like_count;default:0"`                // 点赞数
	ReplyCount     int        `json:"reply_count" gorm:"column:reply_count;default:0"`              // 子评论数
	Status         int16      `json:"status" gorm:"column:status;type:smallint;default:1"`          // 状态
	Deleted        int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`        // 逻辑删除
	CreateTime     time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime     time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
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

// CreateComment 创建评论（事务内更新根评论回复计数）
// 注意：帖子评论计数由Redis+Kafka异步处理，不再在事务内直接更新数据库
func CreateComment(db *gorm.DB, comment *Comment) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. 插入评论
		if err := tx.Create(comment).Error; err != nil {
			return err
		}

		// 2. 如果是回复（root_id > 0），增加根评论的回复计数
		if comment.RootID > 0 {
			if err := tx.Model(&Comment{}).Where("id = ?", comment.RootID).
				UpdateColumn("reply_count", gorm.Expr("reply_count + ?", 1)).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

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

// --- 游标分页 ---

// encodeCursor 将 map 编码为 base64 游标字符串
func encodeCursor(values map[string]interface{}) string {
	data, _ := json.Marshal(values)
	return base64.StdEncoding.EncodeToString(data)
}

// decodeCursor 将 base64 游标字符串解码为 map
func decodeCursor(cursor string) (map[string]interface{}, error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// buildNextCursor 根据评论和排序方式构建下一页游标
func buildNextCursor(comment *Comment, sort int) string {
	switch sort {
	case 0: // 按点赞
		return encodeCursor(map[string]interface{}{
			"like_count": float64(comment.LikeCount),
			"id":         float64(comment.ID),
		})
	case 1: // 按时间
		return encodeCursor(map[string]interface{}{
			"id": float64(comment.ID),
		})
	}
	return ""
}

// applyCursorCondition 根据游标和排序方式添加 WHERE 条件
func applyCursorCondition(query *gorm.DB, cursor string, sort int) (*gorm.DB, error) {
	if cursor == "" {
		return query, nil
	}

	values, err := decodeCursor(cursor)
	if err != nil {
		return nil, err
	}

	switch sort {
	case 0: // 按点赞倒序：keyset (like_count, id)
		likeCount := int64(values["like_count"].(float64))
		id := int64(values["id"].(float64))
		query = query.Where(
			"(like_count < ?) OR (like_count = ? AND id < ?)",
			likeCount, likeCount, id,
		)
	case 1: // 按时间倒序：id DESC
		id := int64(values["id"].(float64))
		query = query.Where("id < ?", id)
	}

	return query, nil
}

// applyOrderBy 根据排序方式添加 ORDER BY
func applyOrderBy(query *gorm.DB, sort int) *gorm.DB {
	switch sort {
	case 0: // 按点赞倒序
		return query.Order("like_count DESC, id DESC")
	case 1: // 按时间倒序
		return query.Order("id DESC")
	}
	return query.Order("id DESC")
}

// GetRootCommentsByCursor 游标分页获取帖子的顶层评论
// sort: 0=按点赞倒序, 1=按时间倒序
// 返回评论列表、下一页游标、是否有更多、错误
func GetRootCommentsByCursor(db *gorm.DB, postID int64, size, sort int, cursor string) ([]Comment, string, bool, error) {
	query := db.Model(&Comment{}).Where("post_id = ? AND root_id = 0 AND deleted = 0", postID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []Comment
	if err := query.Limit(size + 1).Find(&comments).Error; err != nil {
		return nil, "", false, err
	}

	hasMore := len(comments) > size
	if hasMore {
		comments = comments[:size]
	}

	// 构建下一页游标
	var nextCursor string
	if hasMore && len(comments) > 0 {
		nextCursor = buildNextCursor(&comments[len(comments)-1], sort)
	}

	return comments, nextCursor, hasMore, nil
}

// GetRepliesByCursor 游标分页获取某条评论的子回复
// sort: 0=按时间倒序, 1=按点赞倒序
// 返回评论列表、下一页游标、是否有更多、错误
func GetRepliesByCursor(db *gorm.DB, rootID int64, size, sort int, cursor string) ([]Comment, string, bool, error) {
	query := db.Model(&Comment{}).Where("root_id = ? AND deleted = 0", rootID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []Comment
	if err := query.Limit(size + 1).Find(&comments).Error; err != nil {
		return nil, "", false, err
	}

	hasMore := len(comments) > size
	if hasMore {
		comments = comments[:size]
	}

	// 构建下一页游标
	var nextCursor string
	if hasMore && len(comments) > 0 {
		nextCursor = buildNextCursor(&comments[len(comments)-1], sort)
	}

	return comments, nextCursor, hasMore, nil
}
