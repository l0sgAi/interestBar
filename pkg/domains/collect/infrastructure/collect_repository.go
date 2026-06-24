package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"interestBar/pkg/domains/collect/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postCollectRepoGORM 基于 GORM 的 PostCollectRepository 实现。
type postCollectRepoGORM struct {
	db *gorm.DB
}

// NewPostCollectRepository 构造 PostCollectRepository。
func NewPostCollectRepository(db *gorm.DB) domain.PostCollectRepository {
	return &postCollectRepoGORM{db: db}
}

func (r *postCollectRepoGORM) IsCollected(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, domain.PostCollectActive).
		Count(&count).Error
	return count > 0, err
}

// collectCursor keyset 游标（base64 编码的 JSON， opaque to client）。
type collectCursor struct {
	CreateTime time.Time `json:"t"`
	ID         uuid.UUID `json:"i"`
}

// ListCollectedPostIDs 按收藏时间倒序 keyset 分页。
//
// 游标语义：(create_time, id) 复合比较，配合索引 idx_pcollect_user_active
// (user_id, create_time DESC, id DESC) WHERE deleted=0 实现无 OFFSET 深翻页。
// 取 size+1 条判断是否还有下一页；nextCursor 编码本页最后一条。
func (r *postCollectRepoGORM) ListCollectedPostIDs(ctx context.Context, userID uuid.UUID, size int, cursor string) ([]uuid.UUID, int64, string, error) {
	q := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("user_id = ? AND deleted = ?", userID, domain.PostCollectActive)

	if cursor != "" {
		c, err := decodeCollectCursor(cursor)
		if err != nil {
			return nil, 0, "", domain.ErrInvalidCursor
		}
		q = q.Where("(create_time, id) < (?, ?)", c.CreateTime, c.ID)
	}

	var rows []domain.PostCollect
	if err := q.Order("create_time DESC, id DESC").Limit(size + 1).Find(&rows).Error; err != nil {
		return nil, 0, "", err
	}

	var total int64
	if err := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("user_id = ? AND deleted = ?", userID, domain.PostCollectActive).
		Count(&total).Error; err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if len(rows) > size {
		rows = rows[:size]
		last := rows[len(rows)-1]
		nextCursor = encodeCollectCursor(last.CreateTime, last.ID)
	}

	postIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		postIDs = append(postIDs, row.PostID)
	}
	return postIDs, total, nextCursor, nil
}

func encodeCollectCursor(t time.Time, id uuid.UUID) string {
	b, _ := json.Marshal(collectCursor{CreateTime: t, ID: id})
	return base64.URLEncoding.EncodeToString(b)
}

func decodeCollectCursor(s string) (*collectCursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c collectCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.ID == uuid.Nil {
		return nil, errInvalidCursor
	}
	return &c, nil
}

var errInvalidCursor = &invalidCursorError{}

type invalidCursorError struct{}

func (e *invalidCursorError) Error() string { return "invalid collect cursor" }
