package domain

import (
	"context"

	"github.com/google/uuid"
)

// NotificationRepository 是 notice 领域的持久化接口（仅读侧 + 已读操作；
// 写入由 Redpanda consumer 直写 DB，见 notification_consumer.go）。
type NotificationRepository interface {
	// ListByCursor keyset 游标分页获取用户通知（ORDER BY id DESC，UUIDv7 字典序==时间序）。
	// noticeType=0 表示全部类型；cursor="" 表示第一页。
	// 返回通知列表、下一页游标（无更多时为 ""）、错误。
	ListByCursor(ctx context.Context, recipientID uuid.UUID, noticeType int16, size int, cursor string) ([]Notification, string, error)
	// CountUnread 统计用户未读通知数（缓存 miss 回源用）。
	CountUnread(ctx context.Context, recipientID uuid.UUID) (int64, error)
	// MarkRead 批量标记已读（仅本人 + 未读行）。返回实际更新行数（供计数器 DECRBY）。
	MarkRead(ctx context.Context, recipientID uuid.UUID, ids []uuid.UUID) (int64, error)
	// MarkAllRead 全部标记已读。
	MarkAllRead(ctx context.Context, recipientID uuid.UUID) error
}

// NoticeUnreadCache 未读通知计数器缓存（Redis String）。
//
// 计数器是软信号：miss 回源 DB COUNT 回填；INCRBY/DECRBY 漂移由
// TTL 到期 recount + read-all SET 0 自愈（无锁无单飞，同 stats 哲学）。
type NoticeUnreadCache interface {
	// Get 读取未读计数。miss 返回 ok=false（不视为错误）。
	Get(ctx context.Context, userID uuid.UUID) (count int64, ok bool, err error)
	// IncrBy 累加未读计数（consumer upsert 后调用）。key 不存在时从 0 起累加并设 TTL。
	IncrBy(ctx context.Context, userID uuid.UUID, delta int64) error
	// DecrBy 扣减未读计数（floor 0）；key 不存在时不动作（等读 miss 回源校正）。
	DecrBy(ctx context.Context, userID uuid.UUID, delta int64) error
	// Set 直接设置未读计数（回源回填 / read-all 置 0）。
	Set(ctx context.Context, userID uuid.UUID, count int64) error
}
