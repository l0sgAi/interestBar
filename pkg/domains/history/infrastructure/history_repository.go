package infrastructure

import (
	"context"

	"interestBar/pkg/domains/history/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postViewHistoryRepoGORM 基于 GORM 的 PostHistoryRepository 实现(仅冷启动回源读)。
type postViewHistoryRepoGORM struct {
	db *gorm.DB
}

// NewPostHistoryRepository 构造 PostHistoryRepository。
func NewPostHistoryRepository(db *gorm.DB) domain.PostHistoryRepository {
	return &postViewHistoryRepoGORM{db: db}
}

// ListTopByUserID 按 update_time DESC, id DESC 取该用户最近 size 条浏览历史(postID + 访问时间)。
// 供冷启动回源:ZCARD==0 时从 DB 恢复 top500,update_time 作 ZSET score 回填,保证访问时间一致。
// (update_time, id) 复合排序配合索引 idx_pviewhist_user_time。
func (r *postViewHistoryRepoGORM) ListTopByUserID(ctx context.Context, userID uuid.UUID, size int) ([]domain.ViewEntry, error) {
	var rows []domain.PostViewHistory
	err := r.db.WithContext(ctx).
		Select("post_id, update_time").
		Where("user_id = ?", userID).
		Order("update_time DESC, id DESC").
		Limit(size).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]domain.ViewEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, domain.ViewEntry{PostID: row.PostID, ViewedAt: row.UpdateTime})
	}
	return entries, nil
}
