package rabbitmq

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"sync"
	"time"

	"gorm.io/gorm"
)

// MemberCountAggregator 成员计数聚合器
type MemberCountAggregator struct {
	mu              sync.Mutex
	pendingMessages map[int64]int64 // circle_id -> 累计变化量 (int64防止溢出)
	ticker          *time.Ticker
	stopChan        chan struct{}
	stopped         bool
}

// 使用pipeline的临界值
const redisPipelineThreshold = 20

// NewMemberCountAggregator 创建聚合器
func NewMemberCountAggregator() *MemberCountAggregator {
	return &MemberCountAggregator{
		pendingMessages: make(map[int64]int64),
		ticker:          time.NewTicker(3 * time.Second),
		stopChan:        make(chan struct{}),
	}
}

// StartMemberCountConsumer 启动成员计数消费者
func StartMemberCountConsumer() error {
	if channel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	// 设置 QoS
	err := channel.Qos(
		100,   // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// 开始消费消息
	msgs, err := channel.Consume(
		CircleMemberCountQueue,
		"",    // consumer tag
		false, // auto-ack (手动确认)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	logger.Log.Info("Circle member count consumer started")

	// 创建聚合器
	aggregator := NewMemberCountAggregator()

	// 启动聚合处理协程
	go aggregator.run()

	// 启动消息接收协程
	go func() {
		for d := range msgs {
			var msg JoinMsg
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				logger.Log.Error("Failed to unmarshal join message: " + err.Error())
				d.Ack(false) // 消息格式错误，直接确认，不重试
				continue
			}

			// 将消息添加到聚合器
			aggregator.addMessage(msg.CircleID, int64(msg.IsJoin))
			d.Ack(false) // 确认消息
		}
	}()

	return nil
}

// addMessage 添加消息到待处理队列
func (a *MemberCountAggregator) addMessage(circleID int64, delta int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return
	}
	a.pendingMessages[circleID] += delta
}

// run 运行聚合器，定期批量处理
func (a *MemberCountAggregator) run() {
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

// flush 刷新待处理的消息到数据库和缓存
func (a *MemberCountAggregator) flush() {
	a.mu.Lock()
	if len(a.pendingMessages) == 0 {
		a.mu.Unlock()
		return
	}

	messages := a.pendingMessages
	a.pendingMessages = make(map[int64]int64)
	a.mu.Unlock()

	logger.Log.Info(fmt.Sprintf(
		"Flushing %d circle member count updates",
		len(messages),
	))

	if err := a.batchUpdateMemberCount(messages); err != nil {
		logger.Log.Error("Batch update member count failed: " + err.Error())
	}
}

// batchUpdateMemberCount 批量更新圈子成员计数到数据库（持久化）
// Redis缓存已在加入/退出时通过INCR/DECR实时更新，此处仅持久化到数据库
func (a *MemberCountAggregator) batchUpdateMemberCount(deltas map[int64]int64) error {
	return pgsql.DB.Transaction(func(tx *gorm.DB) error {

		// 1. 构造 VALUES 列表
		type row struct {
			CircleID int64 `json:"circle_id"`
			Delta    int64 `json:"delta"`
		}

		rows := make([]row, 0, len(deltas))
		for id, delta := range deltas {
			if delta != 0 {
				rows = append(rows, row{id, delta})
			}
		}

		if len(rows) == 0 {
			return nil
		}

		// 2. 批量更新 member_count（数据库作为持久化层）
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
			return err
		}

		if err := tx.Exec(sql, string(jsonBytes)).Error; err != nil {
			return err
		}

		logger.Log.Info(
			fmt.Sprintf("Successfully persisted %d circle member count updates to database", len(rows)),
		)

		return nil
	})
}

// 辅助函数
func keys(m map[int64]int64) []int64 {
	ids := make([]int64, 0, len(m))
	for k := range m {
		ids = append(ids, k)
	}
	return ids
}

// Stop 停止聚合器
func (a *MemberCountAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()

	close(a.stopChan)
}

// StartMemberCountConsumerWithRetry 启动消费者，带重试机制
func StartMemberCountConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartMemberCountConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start member count consumer (attempt %d/%d): %s", attempt, maxAttempts, err.Error()))

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
			logger.Log.Info("Member count consumer started successfully, exiting retry loop")
			return
		}
	}
}
