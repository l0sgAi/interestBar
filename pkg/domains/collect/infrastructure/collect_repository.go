package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"interestBar/pkg/domains/collect/domain"
	sharedomain "interestBar/pkg/shared/domain"

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

// SetCollected 同步 upsert 收藏流水行（供 Toggle 即时入库）。
//
// 复用 collect_consumer.batchUpdatePostCollects 的单条 upsert 语义：
//   - 行存在 → UPDATE deleted 状态（active=true 恢复 / active=false 取消）；
//   - 行不存在且 active=true → CREATE（PK 调 sharedomain.NewID()），并发下吞 duplicate key；
//   - 行不存在且 active=false → no-op（无行可标，等价 0 行 UPDATE）。
func (r *postCollectRepoGORM) SetCollected(ctx context.Context, userID, postID uuid.UUID, active bool) error {
	db := r.db.WithContext(ctx)

	var existing domain.PostCollect
	err := db.Where("user_id = ? AND post_id = ?", userID, postID).First(&existing).Error
	if err == nil {
		deleted := domain.PostCollectActive
		if !active {
			deleted = domain.PostCollectCanceled
		}
		return db.Model(&domain.PostCollect{}).Where("id = ?", existing.ID).Update("deleted", deleted).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// 无流水行：仅收藏时新建，取消收藏 no-op
	if !active {
		return nil
	}
	err = db.Create(&domain.PostCollect{
		ID:      sharedomain.NewID(),
		UserID:  userID,
		PostID:  postID,
		Deleted: domain.PostCollectActive,
	}).Error
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return nil // 并发下对手已插入，幂等成功
	}
	if err != nil {
		return fmt.Errorf("failed to create post collect: %w", err)
	}
	return nil
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
//
// keyword 非空时 JOIN domains.post 过滤 title/summary（ILIKE %kw%）+ 仅已发布未删帖，
// 游标与排序仍基于 post_collect 列（JOIN 后列名歧义，故全限定表名）。
func (r *postCollectRepoGORM) ListCollectedPostIDs(ctx context.Context, userID uuid.UUID, keyword string, size int, cursor string) ([]uuid.UUID, int64, string, error) {
	keyword = strings.TrimSpace(keyword)

	q := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("domains.post_collect.user_id = ? AND domains.post_collect.deleted = ?", userID, domain.PostCollectActive)

	// 关键字过滤：JOIN domains.post + title/summary ILIKE + 仅已发布未删帖
	if keyword != "" {
		q = q.Joins("JOIN domains.post ON domains.post.id = domains.post_collect.post_id").
			Where("domains.post.deleted = ? AND domains.post.status = ?", 0, 1).
			Where("(domains.post.title ILIKE ? OR domains.post.summary ILIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}

	if cursor != "" {
		c, err := decodeCollectCursor(cursor)
		if err != nil {
			return nil, 0, "", domain.ErrInvalidCursor
		}
		q = q.Where("(domains.post_collect.create_time, domains.post_collect.id) < (?, ?)", c.CreateTime, c.ID)
	}

	// JOIN 后 SELECT * 会带入 domains.post 同名列(id/create_time 等)，显式取 post_collect 列避免歧义
	var rows []domain.PostCollect
	if err := q.Select("domains.post_collect.id, domains.post_collect.user_id, domains.post_collect.post_id, domains.post_collect.deleted, domains.post_collect.create_time, domains.post_collect.update_time").
		Order("domains.post_collect.create_time DESC, domains.post_collect.id DESC").
		Limit(size + 1).
		Find(&rows).Error; err != nil {
		return nil, 0, "", err
	}

	var total int64
	countQ := r.db.WithContext(ctx).Model(&domain.PostCollect{}).
		Where("domains.post_collect.user_id = ? AND domains.post_collect.deleted = ?", userID, domain.PostCollectActive)
	if keyword != "" {
		countQ = countQ.Joins("JOIN domains.post ON domains.post.id = domains.post_collect.post_id").
			Where("domains.post.deleted = ? AND domains.post.status = ?", 0, 1).
			Where("(domains.post.title ILIKE ? OR domains.post.summary ILIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	if err := countQ.Count(&total).Error; err != nil {
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
