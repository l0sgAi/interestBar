package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"interestBar/pkg/conf"
	noticedomain "interestBar/pkg/domains/notice/domain"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	redispkg "interestBar/pkg/server/storage/redis"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// 通知事件类型 → domains.notification.notice_type 映射。
var noticeTypeCode = map[string]int16{
	NoticeTypeLikePost:     noticedomain.NoticeTypeLikePost,
	NoticeTypeLikeComment:  noticedomain.NoticeTypeLikeComment,
	NoticeTypeCollectPost:  noticedomain.NoticeTypeCollectPost,
	NoticeTypeCommentPost:  noticedomain.NoticeTypeCommentPost,
	NoticeTypeReplyComment: noticedomain.NoticeTypeReplyComment,
	NoticeTypeMention:      noticedomain.NoticeTypeMention,
}

// noticeSnippetMaxRunes snippet 快照上限（与 VARCHAR(200) 对齐留余量）。
const noticeSnippetMaxRunes = 100

// NotificationEventAggregator 通知事件聚合器。
//
// 与统计聚合器不同：不做 delta 合并（每条事件都可能产生一行通知），
// 只在 flush 窗口内收集事件，批量反查接收人 + 规则过滤 + upsert。
type NotificationEventAggregator struct {
	mu       sync.Mutex
	events   []NotificationEventMessage
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

// StartNotificationEventConsumer 启动通知事件消费者。
func StartNotificationEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing notification event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.NoticeEventTopic,
		GroupID:        conf.Config.Redpanda.NoticeEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Notification event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.NoticeEventTopic, conf.Config.Redpanda.NoticeEventConsumerGroup))

	flushInterval := conf.Config.Redpanda.NoticeEventFlushInterval
	if flushInterval <= 0 {
		flushInterval = 5 // 兜底 5 秒
	}
	aggregator := &NotificationEventAggregator{
		events:   make([]NotificationEventMessage, 0, 256),
		ticker:   time.NewTicker(time.Duration(flushInterval) * time.Second),
		stopChan: make(chan struct{}),
	}

	go aggregator.run()

	go func() {
		defer r.Close()
		for {
			msg, err := r.ReadMessage(context.Background())
			if err != nil {
				errStr := err.Error()
				if containsIgnoreCase(errStr, "no data") ||
					containsIgnoreCase(errStr, "multiple Read calls return no data") ||
					containsIgnoreCase(errStr, "context deadline exceeded") ||
					containsIgnoreCase(errStr, "timeout") {
					logger.Log.Debug("No messages in notification event queue, waiting...")
					time.Sleep(30 * time.Minute)
					continue
				}
				logger.Log.Error("Failed to read notification event message: " + errStr)
				time.Sleep(5 * time.Second)
				continue
			}

			var event NotificationEventMessage
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				logger.Log.Error("Failed to unmarshal notification event: " + err.Error())
				continue
			}

			aggregator.addMessage(event)
		}
	}()

	return nil
}

func (a *NotificationEventAggregator) addMessage(msg NotificationEventMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.events = append(a.events, msg)
}

func (a *NotificationEventAggregator) run() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.stopChan:
			a.ticker.Stop()
			a.flush()
			return
		}
	}
}

// StopNotificationEventConsumer 优雅排干（幂等）。在关停流程中调用。
func (a *NotificationEventAggregator) StopNotificationEventConsumer() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()
	close(a.stopChan)
}

func (a *NotificationEventAggregator) flush() {
	a.mu.Lock()
	if len(a.events) == 0 {
		a.mu.Unlock()
		return
	}
	events := a.events
	a.events = make([]NotificationEventMessage, 0, 256)
	a.mu.Unlock()

	rows, unreadDeltas := buildNotificationRows(events)
	if len(rows) == 0 {
		return
	}

	if err := upsertNotifications(rows); err != nil {
		logger.Log.Error("Failed to batch upsert notifications: " + err.Error())
		return
	}
	incrNoticeUnread(unreadDeltas)
	logger.Log.Info(fmt.Sprintf("Successfully upserted %d notifications", len(rows)))
}

// ===== 接收人解析与规则过滤 =====

// noticeRow 待 upsert 的通知行（jsonb_to_recordset 载荷）。
type noticeRow struct {
	ID          uuid.UUID  `json:"id"`
	RecipientID uuid.UUID  `json:"recipient_id"`
	ActorID     uuid.UUID  `json:"actor_id"`
	NoticeType  int16      `json:"notice_type"`
	PostID      *uuid.UUID `json:"post_id"`
	CommentID   *uuid.UUID `json:"comment_id"`
	Snippet     string     `json:"snippet"`
}

// postLookupRow 接收人/标题反查行（.Table 原始表名，避免跨域 import 实体）。
type postLookupRow struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Title  string
}

// commentLookupRow 评论反查行。
type commentLookupRow struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	ReplyToUserID *uuid.UUID
	Content       string
}

// buildNotificationRows 把一批事件解析成通知行 + 每接收人未读增量。
//
// 规则落点（设计 docs/notice-design.md §3.3）：
//   - 接收人消费端批量反查（post/comment 各一次 IN 查询；mention 自带接收人）
//   - R1 自动作不通知；R4 同 (recipient, comment) mention 优先；批内全键去重
//   - 目标已删（反查 miss）→ 丢弃
func buildNotificationRows(events []NotificationEventMessage) ([]noticeRow, map[uuid.UUID]int64) {
	postIDs, commentIDs := collectLookupIDs(events)
	posts := lookupPosts(postIDs)
	comments := lookupComments(commentIDs)

	// dedup 键: recipient|actor|type|post|comment（与 uk_notice_dedup 对齐）
	dedup := make(map[string]*noticeRow, len(events))
	// R4: (recipient|comment) → 是否已有 mention
	mentionIdx := make(map[string]bool)
	// 非 mention 候选，待 R4 过滤后合并
	type candidate struct {
		key string
		row *noticeRow
		r4  string // recipient|comment 键（无 comment 时含 post 防误伤）
	}
	var candidates []candidate

	addRow := func(recipient uuid.UUID, event NotificationEventMessage, snippet string) {
		// R1 自动作不通知
		if recipient == uuid.Nil || recipient == event.ActorID {
			return
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%s",
			recipient, event.ActorID, event.Type, uuidPtrStr(event.PostID), uuidPtrStr(event.CommentID))
		if _, exists := dedup[key]; exists {
			return
		}
		row := &noticeRow{
			ID:          sharedomain.NewID(),
			RecipientID: recipient,
			ActorID:     event.ActorID,
			NoticeType:  noticeTypeCode[event.Type],
			PostID:      event.PostID,
			CommentID:   event.CommentID,
			Snippet:     truncateRunes(snippet, noticeSnippetMaxRunes),
		}
		dedup[key] = row
		r4Key := fmt.Sprintf("%s|%s", recipient, uuidPtrStr(event.CommentID))
		if event.Type == NoticeTypeMention {
			mentionIdx[r4Key] = true
		}
		candidates = append(candidates, candidate{key: key, row: row, r4: r4Key})
	}

	for _, event := range events {
		code, ok := noticeTypeCode[event.Type]
		if !ok || code == 0 {
			logger.Log.Error("Unknown notification event type: " + event.Type)
			continue
		}

		switch event.Type {
		case NoticeTypeLikePost, NoticeTypeCollectPost, NoticeTypeCommentPost:
			if event.PostID == nil {
				continue
			}
			post, ok := posts[*event.PostID]
			if !ok {
				continue // 目标已删
			}
			snippet := event.Snippet
			if event.Type != NoticeTypeCommentPost {
				snippet = post.Title // like/collect 快照帖子标题（确认项 #4）
			}
			addRow(post.UserID, event, snippet)

		case NoticeTypeLikeComment:
			if event.CommentID == nil {
				continue
			}
			cm, ok := comments[*event.CommentID]
			if !ok {
				continue
			}
			addRow(cm.UserID, event, cm.Content)

		case NoticeTypeReplyComment:
			if event.CommentID == nil {
				continue
			}
			cm, ok := comments[*event.CommentID]
			if !ok || cm.ReplyToUserID == nil {
				continue
			}
			addRow(*cm.ReplyToUserID, event, event.Snippet)

		case NoticeTypeMention:
			for _, recipient := range event.MentionUserIDs {
				addRow(recipient, event, event.Snippet)
			}
		}
	}

	// R4 过滤 + 汇总
	rows := make([]noticeRow, 0, len(candidates))
	unreadDeltas := make(map[uuid.UUID]int64)
	for _, c := range candidates {
		// 同 (recipient, comment) 已有 mention → 丢弃 reply/comment_post（mention 优先）
		if c.row.NoticeType != noticedomain.NoticeTypeMention && c.row.CommentID != nil && mentionIdx[c.r4] {
			delete(dedup, c.key)
			continue
		}
		rows = append(rows, *c.row)
		unreadDeltas[c.row.RecipientID]++
	}
	return rows, unreadDeltas
}

// collectLookupIDs 收集本批事件需要反查的 post/comment ID。
func collectLookupIDs(events []NotificationEventMessage) ([]uuid.UUID, []uuid.UUID) {
	postSet := make(map[uuid.UUID]struct{})
	commentSet := make(map[uuid.UUID]struct{})
	for _, e := range events {
		switch e.Type {
		case NoticeTypeLikePost, NoticeTypeCollectPost, NoticeTypeCommentPost:
			if e.PostID != nil {
				postSet[*e.PostID] = struct{}{}
			}
		case NoticeTypeLikeComment, NoticeTypeReplyComment:
			if e.CommentID != nil {
				commentSet[*e.CommentID] = struct{}{}
			}
		}
	}
	return uuidSetSlice(postSet), uuidSetSlice(commentSet)
}

// lookupPosts 批量反查帖子作者与标题。
func lookupPosts(postIDs []uuid.UUID) map[uuid.UUID]postLookupRow {
	result := make(map[uuid.UUID]postLookupRow, len(postIDs))
	if len(postIDs) == 0 {
		return result
	}
	var rows []postLookupRow
	if err := pgsql.DB.WithContext(context.Background()).
		Table("domains.post").
		Select("id, user_id, title").
		Where("id IN ? AND deleted = ?", postIDs, 0).
		Scan(&rows).Error; err != nil {
		logger.Log.Error("Failed to lookup posts for notifications: " + err.Error())
		return result
	}
	for _, r := range rows {
		result[r.ID] = r
	}
	return result
}

// lookupComments 批量反查评论作者/被回复人/正文。
func lookupComments(commentIDs []uuid.UUID) map[uuid.UUID]commentLookupRow {
	result := make(map[uuid.UUID]commentLookupRow, len(commentIDs))
	if len(commentIDs) == 0 {
		return result
	}
	var rows []commentLookupRow
	if err := pgsql.DB.WithContext(context.Background()).
		Table("domains.comment").
		Select("id, user_id, reply_to_user_id, content").
		Where("id IN ? AND deleted = ?", commentIDs, 0).
		Scan(&rows).Error; err != nil {
		logger.Log.Error("Failed to lookup comments for notifications: " + err.Error())
		return result
	}
	for _, r := range rows {
		result[r.ID] = r
	}
	return result
}

// upsertNotifications 批量 upsert（uk_notice_dedup 表达式索引锚点）。
//
// 冲突（重赞/at-least-once 重复投递）→ 复用行：重置未读 + 刷新 snippet/时间，不产生第二行。
func upsertNotifications(rows []noticeRow) error {
	jsonBytes, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("failed to marshal notification rows: %w", err)
	}
	sql := `INSERT INTO domains.notification (id, recipient_id, actor_id, notice_type, post_id, comment_id, snippet)
		SELECT * FROM jsonb_to_recordset(?::jsonb)
		AS v(id uuid, recipient_id uuid, actor_id uuid, notice_type smallint, post_id uuid, comment_id uuid, snippet varchar)
		ON CONFLICT (recipient_id, actor_id, notice_type,
			COALESCE(post_id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(comment_id, '00000000-0000-0000-0000-000000000000'::uuid))
		WHERE deleted = 0
		DO UPDATE SET is_read = 0, snippet = EXCLUDED.snippet, update_time = CURRENT_TIMESTAMP`
	return pgsql.DB.Exec(sql, string(jsonBytes)).Error
}

// incrNoticeUnread 按接收人聚合累加未读计数（best-effort，软信号）。
func incrNoticeUnread(deltas map[uuid.UUID]int64) {
	if len(deltas) == 0 {
		return
	}
	ctx := context.Background()
	pipe := redispkg.Client.Pipeline()
	for userID, delta := range deltas {
		key := redispkg.GetNoticeUnreadKey(userID)
		pipe.IncrBy(ctx, key, delta)
		pipe.Expire(ctx, key, redispkg.NoticeUnreadTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Log.Error("Failed to incr notice unread counters: " + err.Error())
	}
}

// ===== 小工具 =====

func uuidPtrStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func uuidSetSlice(set map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// StartNotificationEventConsumerWithRetry 启动通知事件消费者，带重试机制。
func StartNotificationEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartNotificationEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start notification event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for notification event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying notification event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Notification event consumer started successfully")
			return
		}
	}
}
