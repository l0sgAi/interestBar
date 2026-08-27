// Package infrastructure 提供 notice 领域基础设施层实现。
package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"interestBar/pkg/domains/notice/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// notificationRepoPG 基于 GORM/PostgreSQL 的 NotificationRepository 实现。
type notificationRepoPG struct {
	db *gorm.DB
}

// NewNotificationRepository 构造 NotificationRepository。
func NewNotificationRepository(db *gorm.DB) domain.NotificationRepository {
	return &notificationRepoPG{db: db}
}

// ListByCursor keyset 游标分页（ORDER BY id DESC，游标条件 id < cursor.id）。
//
// noticeTypes 非空时按 notice_type IN (...) 过滤；多取一条探测 hasMore；
// 返回的游标取本页最后一条的 id。
func (r *notificationRepoPG) ListByCursor(ctx context.Context, recipientID uuid.UUID, noticeTypes []int16, size int, cursor string) ([]domain.Notification, string, error) {
	query := r.db.WithContext(ctx).
		Where("recipient_id = ? AND deleted = ?", recipientID, 0)
	if len(noticeTypes) > 0 {
		query = query.Where("notice_type IN ?", noticeTypes)
	}

	if cursor != "" {
		cursorID, err := decodeNoticeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query = query.Where("id < ?", cursorID)
	}

	var notices []domain.Notification
	if err := query.Order("id DESC").Limit(size + 1).Find(&notices).Error; err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(notices) > size {
		notices = notices[:size]
		nextCursor = encodeNoticeCursor(notices[len(notices)-1].ID)
	}
	return notices, nextCursor, nil
}

// CountUnread 统计未读通知数。
func (r *notificationRepoPG) CountUnread(ctx context.Context, recipientID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("recipient_id = ? AND is_read = ? AND deleted = ?", recipientID, domain.NoticeUnread, 0).
		Count(&count).Error
	return count, err
}

// MarkRead 批量标记已读（仅本人 + 未读行），返回实际更新行数。
func (r *notificationRepoPG) MarkRead(ctx context.Context, recipientID uuid.UUID, ids []uuid.UUID) (int64, error) {
	res := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("recipient_id = ? AND id IN ? AND is_read = ? AND deleted = ?", recipientID, ids, domain.NoticeUnread, 0).
		Updates(map[string]interface{}{
			"is_read":     domain.NoticeRead,
			"update_time": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return res.RowsAffected, res.Error
}

// MarkAllRead 全部标记已读。
func (r *notificationRepoPG) MarkAllRead(ctx context.Context, recipientID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("recipient_id = ? AND is_read = ? AND deleted = ?", recipientID, domain.NoticeUnread, 0).
		Updates(map[string]interface{}{
			"is_read":     domain.NoticeRead,
			"update_time": gorm.Expr("CURRENT_TIMESTAMP"),
		}).Error
}

// ===== 游标工具函数（单字段 id keyset，仿 comment 域 base64 JSON 风格）=====

// encodeNoticeCursor 编码游标：base64 JSON {"id": "<uuid>"}。
func encodeNoticeCursor(id uuid.UUID) string {
	data, _ := json.Marshal(map[string]interface{}{"id": id.String()})
	return base64.StdEncoding.EncodeToString(data)
}

// decodeNoticeCursor 解码并校验游标；非法输入包装 ErrInvalidCursor（绝不 panic）。
func decodeNoticeCursor(cursor string) (uuid.UUID, error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: decode failed: %v", domain.ErrInvalidCursor, err)
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return uuid.Nil, fmt.Errorf("%w: unmarshal failed: %v", domain.ErrInvalidCursor, err)
	}
	raw, ok := values["id"].(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: missing id", domain.ErrInvalidCursor)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: bad id: %v", domain.ErrInvalidCursor, err)
	}
	return id, nil
}
