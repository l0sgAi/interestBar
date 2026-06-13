package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/model"
	"interestBar/pkg/server/storage/db/pgsql"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// likeEventDelta 点赞事件增量
type likeEventDelta struct {
	EventType string
	UserID    uuid.UUID
	TargetID  uuid.UUID
	PostID    uuid.UUID
	Amount    int64
}

// LikeEventAggregator 点赞事件聚合器
type LikeEventAggregator struct {
	mu       sync.Mutex
	deltas   map[string]*likeEventDelta
	ticker   *time.Ticker
	stopChan chan struct{}
	stopped  bool
}

func likeDeltaKey(eventType string, userID, targetID uuid.UUID) string {
	return fmt.Sprintf("%s:%s:%s", eventType, userID.String(), targetID.String())
}

// StartLikeEventConsumer 启动点赞事件消费者
func StartLikeEventConsumer() error {
	brokers := conf.Config.Redpanda.Brokers
	logger.Log.Info(fmt.Sprintf("Initializing like event consumer with brokers: %v", brokers))

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		Resolver:  nil,
	}

	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          conf.Config.Redpanda.LikeEventTopic,
		GroupID:        conf.Config.Redpanda.LikeEventConsumerGroup,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		Dialer:         dialer,
	})

	logger.Log.Info(fmt.Sprintf("Like event consumer created: topic=%s, group=%s",
		conf.Config.Redpanda.LikeEventTopic, conf.Config.Redpanda.LikeEventConsumerGroup))

	aggregator := &LikeEventAggregator{
		deltas:   make(map[string]*likeEventDelta),
		ticker:   time.NewTicker(time.Duration(conf.Config.Redpanda.LikeEventFlushInterval) * time.Second),
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
					logger.Log.Debug("No messages in like event queue, waiting...")
					time.Sleep(30 * time.Minute)
					continue
				}
				logger.Log.Error("Failed to read like event message: " + errStr)
				time.Sleep(5 * time.Second)
				continue
			}

			var likeMsg LikeEventMessage
			if err := json.Unmarshal(msg.Value, &likeMsg); err != nil {
				logger.Log.Error("Failed to unmarshal like event: " + err.Error())
				continue
			}

			aggregator.addMessage(likeMsg)
		}
	}()

	return nil
}

func (a *LikeEventAggregator) addMessage(msg LikeEventMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}

	key := likeDeltaKey(msg.Type, msg.UserID, msg.TargetID)
	delta, exists := a.deltas[key]
	if !exists {
		delta = &likeEventDelta{
			EventType: msg.Type,
			UserID:    msg.UserID,
			TargetID:  msg.TargetID,
			PostID:    msg.PostID,
		}
		a.deltas[key] = delta
	}
	delta.Amount += msg.Amount
}

func (a *LikeEventAggregator) run() {
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

func (a *LikeEventAggregator) flush() {
	a.mu.Lock()
	if len(a.deltas) == 0 {
		a.mu.Unlock()
		return
	}
	deltas := a.deltas
	a.deltas = make(map[string]*likeEventDelta)
	a.mu.Unlock()

	commentDeltas := make([]*likeEventDelta, 0)
	postDeltas := make([]*likeEventDelta, 0)
	for _, d := range deltas {
		if d.Amount == 0 {
			continue
		}
		switch d.EventType {
		case "comment_like":
			commentDeltas = append(commentDeltas, d)
		case "post_like":
			postDeltas = append(postDeltas, d)
		}
	}

	if len(commentDeltas) > 0 {
		if err := batchUpdateCommentLikes(commentDeltas); err != nil {
			logger.Log.Error("Failed to batch update comment likes: " + err.Error())
		}
	}
	if len(postDeltas) > 0 {
		if err := batchUpdatePostLikes(postDeltas); err != nil {
			logger.Log.Error("Failed to batch update post likes: " + err.Error())
		}
	}
}

func batchUpdateCommentLikes(deltas []*likeEventDelta) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		for _, d := range deltas {
			if d.Amount > 0 {
				var existing model.CommentLike
				err := tx.Where("user_id = ? AND comment_id = ?", d.UserID, d.TargetID).First(&existing).Error
				if err == gorm.ErrRecordNotFound {
					if err := tx.Create(&model.CommentLike{
						UserID:    d.UserID,
						CommentID: d.TargetID,
						PostID:    &d.PostID,
						Deleted:   model.CommentLikeActive,
					}).Error; err != nil {
						if !strings.Contains(err.Error(), "duplicate key") {
							return fmt.Errorf("failed to create comment like: %w", err)
						}
					}
				} else if err == nil {
					tx.Model(&model.CommentLike{}).Where("id = ?", existing.ID).Update("deleted", model.CommentLikeActive)
				}
			} else {
				tx.Model(&model.CommentLike{}).
					Where("user_id = ? AND comment_id = ?", d.UserID, d.TargetID).
					Update("deleted", model.CommentLikeCanceled)
			}
		}

		// Aggregate by commentID for like_count update
		commentCountDeltas := make(map[uuid.UUID]int64)
		for _, d := range deltas {
			commentCountDeltas[d.TargetID] += d.Amount
		}

		type row struct {
			CommentID uuid.UUID `json:"comment_id"`
			Delta     int64     `json:"delta"`
		}
		rows := make([]row, 0, len(commentCountDeltas))
		for commentID, delta := range commentCountDeltas {
			if delta != 0 {
				rows = append(rows, row{CommentID: commentID, Delta: delta})
			}
		}
		if len(rows) > 0 {
			sql := `UPDATE comment c SET like_count = GREATEST(c.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
				FROM (SELECT * FROM jsonb_to_recordset(?::jsonb) AS v(comment_id uuid, delta BIGINT)) v
				WHERE c.id = v.comment_id AND c.deleted = 0`
			jsonBytes, _ := json.Marshal(rows)
			if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
				return fmt.Errorf("failed to batch update comment like counts: %w", err)
			}
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d comment likes", len(deltas)))
		return nil
	})
}

func batchUpdatePostLikes(deltas []*likeEventDelta) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		for _, d := range deltas {
			if d.Amount > 0 {
				var existing model.PostLike
				err := tx.Where("user_id = ? AND post_id = ?", d.UserID, d.TargetID).First(&existing).Error
				if err == gorm.ErrRecordNotFound {
					if err := tx.Create(&model.PostLike{
						UserID:  d.UserID,
						PostID:  d.TargetID,
						Deleted: model.PostLikeActive,
					}).Error; err != nil {
						if !strings.Contains(err.Error(), "duplicate key") {
							return fmt.Errorf("failed to create post like: %w", err)
						}
					}
				} else if err == nil {
					tx.Model(&model.PostLike{}).Where("id = ?", existing.ID).Update("deleted", model.PostLikeActive)
				}
			} else {
				tx.Model(&model.PostLike{}).
					Where("user_id = ? AND post_id = ?", d.UserID, d.TargetID).
					Update("deleted", model.PostLikeCanceled)
			}
		}

		// Aggregate by postID for like_count update
		postCountDeltas := make(map[uuid.UUID]int64)
		for _, d := range deltas {
			postCountDeltas[d.TargetID] += d.Amount
		}

		type row struct {
			PostID uuid.UUID `json:"post_id"`
			Delta  int64     `json:"delta"`
		}
		rows := make([]row, 0, len(postCountDeltas))
		for postID, delta := range postCountDeltas {
			if delta != 0 {
				rows = append(rows, row{PostID: postID, Delta: delta})
			}
		}
		if len(rows) > 0 {
			sql := `UPDATE post p SET like_count = GREATEST(p.like_count + v.delta, 0), update_time = CURRENT_TIMESTAMP
				FROM (SELECT * FROM jsonb_to_recordset(?::jsonb) AS v(post_id uuid, delta BIGINT)) v
				WHERE p.id = v.post_id AND p.deleted = 0`
			jsonBytes, _ := json.Marshal(rows)
			if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
				return fmt.Errorf("failed to batch update post like counts: %w", err)
			}
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d post likes", len(deltas)))
		return nil
	})
}

// StartLikeEventConsumerWithRetry 启动点赞事件消费者，带重试机制
func StartLikeEventConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartLikeEventConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start like event consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))
			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for like event consumer, giving up")
				return
			}
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying like event consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			logger.Log.Info("Like event consumer started successfully")
			return
		}
	}
}
