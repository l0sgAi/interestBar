// Package infrastructure 提供 comment 领域基础设施层实现。
package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// commentRepoPG 基于 GORM 的 CommentRepository 实现。
type commentRepoPG struct {
	db *gorm.DB
}

// NewCommentRepository 构造 CommentRepository。
func NewCommentRepository(db *gorm.DB) domain.CommentRepository {
	return &commentRepoPG{db: db}
}

// Create 创建评论（事务内：插入评论 + 如为回复则递增根评论 reply_count）。
//
// 与旧 model.CreateComment 行为一致：
//   - 帖子评论计数由 Redis + Redpanda 异步处理，不在事务内更新数据库。
func (r *commentRepoPG) Create(ctx context.Context, comment *domain.Comment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 插入评论
		if err := tx.Create(comment).Error; err != nil {
			return err
		}

		// 2. 如果是回复（root_id 非空），增加根评论的回复计数
		if comment.RootID != nil {
			if err := tx.Model(&domain.Comment{}).Where("id = ?", *comment.RootID).
				UpdateColumn("reply_count", gorm.Expr("reply_count + ?", 1)).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// GetByID 根据 ID 获取评论（未删除）。
func (r *commentRepoPG) GetByID(ctx context.Context, commentID uuid.UUID) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", commentID, 0).First(&comment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCommentNotFound
		}
		return nil, err
	}
	return &comment, nil
}

// GetRootCommentsByCursor 游标分页获取帖子的顶层评论。
// sort: 0=按点赞倒序, 1=按时间倒序。
func (r *commentRepoPG) GetRootCommentsByCursor(ctx context.Context, postID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	query := r.db.WithContext(ctx).Model(&domain.Comment{}).Where("post_id = ? AND root_id IS NULL AND deleted = 0", postID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []domain.Comment
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

// GetRepliesByCursor 游标分页获取某条评论的子回复。
// sort: 0=按时间倒序, 1=按点赞倒序。
func (r *commentRepoPG) GetRepliesByCursor(ctx context.Context, rootID uuid.UUID, size, sort int, cursor string) ([]domain.Comment, string, bool, error) {
	query := r.db.WithContext(ctx).Model(&domain.Comment{}).Where("root_id = ? AND deleted = 0", rootID)

	// 应用游标条件
	var err error
	query, err = applyCursorCondition(query, cursor, sort)
	if err != nil {
		return nil, "", false, err
	}

	// 排序
	query = applyOrderBy(query, sort)

	// 多查一条判断 has_more
	var comments []domain.Comment
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

// IsLiked 检查用户是否点赞了评论（DB 回源用）。
func (r *commentRepoPG) IsLiked(ctx context.Context, userID, commentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.CommentLike{}).
		Where("user_id = ? AND comment_id = ? AND deleted = ?", userID, commentID, domain.CommentLikeActive).
		Count(&count).Error
	return count > 0, err
}

// BatchCheckLiked 批量检查用户是否点赞了多条评论（DB 回源用）。
func (r *commentRepoPG) BatchCheckLiked(ctx context.Context, userID uuid.UUID, commentIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(commentIDs) == 0 {
		return make(map[uuid.UUID]bool), nil
	}
	var likes []domain.CommentLike
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND comment_id IN ? AND deleted = ?", userID, commentIDs, domain.CommentLikeActive).
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

// ===== 游标工具函数（与旧 model/comment.go 中的实现一致）=====

// encodeCursor 将 map 编码为 base64 游标字符串。
func encodeCursor(values map[string]interface{}) string {
	data, _ := json.Marshal(values)
	return base64.StdEncoding.EncodeToString(data)
}

// decodeCursor 将 base64 游标字符串解码为 map。
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

// buildNextCursor 根据评论和排序方式构建下一页游标。
// 注意：id 使用 UUIDv7 字符串编码(字典序 == 时间序,与 ORDER BY id DESC 配合)。
func buildNextCursor(comment *domain.Comment, sort int) string {
	switch sort {
	case 0: // 按点赞
		return encodeCursor(map[string]interface{}{
			"like_count": float64(comment.LikeCount),
			"id":         comment.ID.String(),
		})
	case 1: // 按时间
		return encodeCursor(map[string]interface{}{
			"id": comment.ID.String(),
		})
	}
	return ""
}

// applyCursorCondition 根据游标和排序方式添加 WHERE 条件。
//
// 游标来自用户可控的 query 参数，必须防御性解析：所有类型断言用 comma-ok，
// 任何字段缺失/类型错误都返回包装了 ErrInvalidCursor 的错误（而非 panic）。
func applyCursorCondition(query *gorm.DB, cursor string, sort int) (*gorm.DB, error) {
	if cursor == "" {
		return query, nil
	}

	likeCount, id, err := parseCursorValues(cursor, sort)
	if err != nil {
		return nil, err
	}

	switch sort {
	case 0: // 按点赞倒序：keyset (like_count, id)
		query = query.Where(
			"(like_count < ?) OR (like_count = ? AND id < ?)",
			likeCount, likeCount, id,
		)
	case 1: // 按时间倒序：id DESC
		query = query.Where("id < ?", id)
	}

	return query, nil
}

// parseCursorValues 解码并校验游标，返回 (likeCount, id, err)。
//
// 抽成纯函数便于单测（无需 gorm）。sort==0 需要 like_count + id，
// sort==1 只需要 id。所有类型断言用 comma-ok，绝不 panic。
// 错误统一用 fmt.Errorf("%w: ...", domain.ErrInvalidCursor, ...) 包装。
func parseCursorValues(cursor string, sort int) (likeCount int64, id uuid.UUID, err error) {
	values, derr := decodeCursor(cursor)
	if derr != nil {
		// base64 / JSON 解析失败统一归为非法游标
		return 0, uuid.Nil, fmt.Errorf("%w: decode failed: %v", domain.ErrInvalidCursor, derr)
	}

	idStr, ok := values["id"].(string)
	if !ok {
		return 0, uuid.Nil, fmt.Errorf("%w: missing or invalid id", domain.ErrInvalidCursor)
	}
	id, perr := uuid.Parse(idStr)
	if perr != nil {
		return 0, uuid.Nil, fmt.Errorf("%w: invalid id: %v", domain.ErrInvalidCursor, perr)
	}

	if sort == 0 {
		likeCountRaw, ok := values["like_count"]
		if !ok {
			return 0, uuid.Nil, fmt.Errorf("%w: missing like_count", domain.ErrInvalidCursor)
		}
		// JSON unmarshal 数字到 map[string]interface{} 会得到 float64
		likeCountF, ok := likeCountRaw.(float64)
		if !ok {
			return 0, uuid.Nil, fmt.Errorf("%w: like_count has wrong type %T", domain.ErrInvalidCursor, likeCountRaw)
		}
		likeCount = int64(likeCountF)
	}

	return likeCount, id, nil
}

// applyOrderBy 根据排序方式添加 ORDER BY。
func applyOrderBy(query *gorm.DB, sort int) *gorm.DB {
	switch sort {
	case 0: // 按点赞倒序
		return query.Order("like_count DESC, id DESC")
	case 1: // 按时间倒序
		return query.Order("id DESC")
	}
	return query.Order("id DESC")
}
