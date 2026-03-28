package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// StatisticsAggregator 统计聚合器
type StatisticsAggregator struct {
	mu              sync.Mutex
	circleCounts    map[int64]int64 // circle_id -> 累计成员变化量
	postCounts      map[int64]int64 // circle_id -> 累计帖子变化量
	ticker          *time.Ticker
	stopChan        chan struct{}
	stopped         bool
}

// StartStatisticsConsumer 启动统计消费者
func StartStatisticsConsumer() error {
	// 创建Kafka Reader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        conf.Config.Redpanda.Brokers,
		Topic:          conf.Config.Redpanda.Topic,
		GroupID:        conf.Config.Redpanda.ConsumerGroup,
		MinBytes:       10e3,  // 10KB
		MaxBytes:       10e6,  // 10MB
		CommitInterval: time.Second, // 自动提交间隔
	})

	logger.Log.Info(fmt.Sprintf("Redpanda consumer started: brokers=%v, topic=%s, group=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.Topic, conf.Config.Redpanda.ConsumerGroup))

	// 创建聚合器
	aggregator := &StatisticsAggregator{
		circleCounts: make(map[int64]int64),
		postCounts:   make(map[int64]int64),
		ticker:       time.NewTicker(time.Duration(conf.Config.Redpanda.FlushInterval) * time.Minute),
		stopChan:     make(chan struct{}),
	}

	// 启动聚合处理协程
	go aggregator.run()

	// 启动消息接收协程
	go func() {
		defer reader.Close()
		for {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				logger.Log.Error("Failed to read message from redpanda: " + err.Error())
				// 短暂等待后重试
				time.Sleep(5 * time.Second)
				continue
			}

			// 解析消息
			var statsMsg CircleStatisticsMessage
			if err := json.Unmarshal(msg.Value, &statsMsg); err != nil {
				logger.Log.Error("Failed to unmarshal statistics message: " + err.Error())
				// 解析失败，跳过该消息（自动提交会处理）
				continue
			}

			// 添加到聚合器
			aggregator.addMessage(statsMsg)
		}
	}()

	return nil
}

// addMessage 添加消息到聚合器
func (a *StatisticsAggregator) addMessage(msg CircleStatisticsMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return
	}

	switch msg.Type {
	case StatisticsTypeCircleCount:
		a.circleCounts[msg.CircleID] += msg.Value
	case StatisticsTypePostCount:
		a.postCounts[msg.CircleID] += msg.Value
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown statistics type: %s", msg.Type))
	}
}

// run 运行聚合器，定期批量处理
func (a *StatisticsAggregator) run() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.stopChan:
			a.ticker.Stop()
			// Stop 时 flush 最后一批数据
			a.flush()
			return
		}
	}
}

// flush 刷新待处理的消息到数据库
func (a *StatisticsAggregator) flush() {
	a.mu.Lock()
	if len(a.circleCounts) == 0 && len(a.postCounts) == 0 {
		a.mu.Unlock()
		return
	}

	circleCounts := a.circleCounts
	postCounts := a.postCounts
	a.circleCounts = make(map[int64]int64)
	a.postCounts = make(map[int64]int64)
	a.mu.Unlock()

	// 统计消息数量
	totalMessages := len(circleCounts) + len(postCounts)
	logger.Log.Info(fmt.Sprintf("Flushing %d circle statistics updates", totalMessages))

	// 分别处理成员计数和帖子计数
	if len(circleCounts) > 0 {
		if err := a.batchUpdateMemberCounts(circleCounts); err != nil {
			logger.Log.Error("Failed to batch update member counts: " + err.Error())
		}
	}

	if len(postCounts) > 0 {
		if err := a.batchUpdatePostCounts(postCounts); err != nil {
			logger.Log.Error("Failed to batch update post counts: " + err.Error())
		}
	}
}

// batchUpdateMemberCounts 批量更新圈子成员计数到数据库
func (a *StatisticsAggregator) batchUpdateMemberCounts(deltas map[int64]int64) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		// 构造更新数据
		type updateRow struct {
			CircleID int64 `json:"circle_id"`
			Delta    int64 `json:"delta"`
		}

		rows := make([]updateRow, 0, len(deltas))
		for circleID, delta := range deltas {
			if delta != 0 {
				rows = append(rows, updateRow{CircleID: circleID, Delta: delta})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 使用JSON批量更新
		sql := `
		UPDATE circle c
		SET member_count = GREATEST(c.member_count + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
			SELECT * FROM jsonb_to_recordset(?::jsonb)
			AS v(circle_id BIGINT, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0
		`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal update rows: %w", err)
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute batch update: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d circle member counts", len(rows)))
		return nil
	})
}

// batchUpdatePostCounts 批量更新圈子帖子计数到数据库
func (a *StatisticsAggregator) batchUpdatePostCounts(deltas map[int64]int64) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {
		// 构造更新数据
		type updateRow struct {
			CircleID int64 `json:"circle_id"`
			Delta    int64 `json:"delta"`
		}

		rows := make([]updateRow, 0, len(deltas))
		for circleID, delta := range deltas {
			if delta != 0 {
				rows = append(rows, updateRow{CircleID: circleID, Delta: delta})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 使用JSON批量更新
		sql := `
		UPDATE circle c
		SET post_count = GREATEST(c.post_count + v.delta, 0),
		    update_time = CURRENT_TIMESTAMP
		FROM (
			SELECT * FROM jsonb_to_recordset(?::jsonb)
			AS v(circle_id BIGINT, delta BIGINT)
		) v
		WHERE c.id = v.circle_id AND c.deleted = 0
		`

		jsonBytes, err := json.Marshal(rows)
		if err != nil {
			return fmt.Errorf("failed to marshal update rows: %w", err)
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return fmt.Errorf("failed to execute batch update: %w", err)
		}

		logger.Log.Info(fmt.Sprintf("Successfully updated %d circle post counts", len(rows)))
		return nil
	})
}

// Stop 停止聚合器
func (a *StatisticsAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()

	close(a.stopChan)
}

// StartStatisticsConsumerWithRetry 启动消费者，带重试机制
func StartStatisticsConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartStatisticsConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start statistics consumer (attempt %d/%d): %s",
				attempt, maxAttempts, err.Error()))

			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached, giving up")
				return
			}

			// 等待后重试
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			// 成功启动，退出重试循环
			logger.Log.Info("Statistics consumer started successfully, exiting retry loop")
			return
		}
	}
}
