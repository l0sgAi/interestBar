package rabbitmq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/db/pgsql"
	"interestBar/pkg/server/storage/elasticsearch"
	"sync"
	"time"
)

// ESMemberCountAggregator ES 成员计数聚合器
type ESMemberCountAggregator struct {
	mu              sync.Mutex
	pendingMessages map[int64]int64 // circle_id -> 累计变化量
	ticker          *time.Ticker
	stopChan        chan struct{}
	stopped         bool
}

// 使用 ES bulk 的临界值
const esBulkThreshold = 20

// NewESMemberCountAggregator 创建 ES 聚合器
func NewESMemberCountAggregator() *ESMemberCountAggregator {
	return &ESMemberCountAggregator{
		pendingMessages: make(map[int64]int64),
		ticker:          time.NewTicker(3 * time.Second),
		stopChan:        make(chan struct{}),
	}
}

// StartESMemberCountConsumer 启动 ES 成员计数消费者
func StartESMemberCountConsumer() error {
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
		CircleMemberCountESQueue,
		"",    // consumer tag
		false, // auto-ack (手动确认)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("failed to register ES consumer: %w", err)
	}

	logger.Log.Info("Circle member count ES consumer started")

	// 创建聚合器
	aggregator := NewESMemberCountAggregator()

	// 启动聚合处理协程
	go aggregator.run()

	// 启动消息接收协程
	go func() {
		for d := range msgs {
			var msg JoinMsg
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				logger.Log.Error("Failed to unmarshal join message for ES: " + err.Error())
				d.Ack(false) // 消息格式错误，直接确认，不重试
				continue
			}

			// 将消息添加到聚合器
			aggregator.addMessage(msg.CircleID, msg.IsJoin)
			d.Ack(false) // 确认消息
		}
	}()

	return nil
}

// addMessage 添加消息到待处理队列
func (a *ESMemberCountAggregator) addMessage(circleID int64, delta int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stopped {
		return
	}
	a.pendingMessages[circleID] += delta
}

// run 运行聚合器，定期批量处理
func (a *ESMemberCountAggregator) run() {
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

// flush 刷新待处理的消息到 ES
func (a *ESMemberCountAggregator) flush() {
	a.mu.Lock()
	if len(a.pendingMessages) == 0 {
		a.mu.Unlock()
		return
	}

	messages := a.pendingMessages
	a.pendingMessages = make(map[int64]int64)
	a.mu.Unlock()

	logger.Log.Info(fmt.Sprintf(
		"Flushing %d circle member count updates to ES",
		len(messages),
	))

	if err := a.batchUpdateESMemberCount(messages); err != nil {
		logger.Log.Error("Batch update ES member count failed: " + err.Error())
	}
}

// batchUpdateESMemberCount 批量更新 ES 中的圈子成员计数
func (a *ESMemberCountAggregator) batchUpdateESMemberCount(deltas map[int64]int64) error {
	if len(deltas) == 0 {
		return nil
	}

	// 1. 从数据库查询圈子的完整信息
	var circles []elasticsearch.CircleDocument
	circleIDs := make([]int64, 0, len(deltas))
	for id := range deltas {
		circleIDs = append(circleIDs, id)
	}

	type dbCircle struct {
		ID          int64
		Name        string
		AvatarURL   string
		Description string
		Hot         int
		CategoryID  int
		MemberCount int
		PostCount   int
		CreateTime  time.Time
		Status      int16
		Deleted     int16
		JoinType    int16
	}

	var dbCircles []dbCircle
	if err := pgsql.DB.Table("circle").
		Select("id, name, avatar_url, description, hot, category_id, member_count, post_count, create_time, status, deleted, join_type").
		Where("id IN ?", circleIDs).
		Find(&dbCircles).Error; err != nil {
		return fmt.Errorf("failed to query circles from database: %w", err)
	}

	// 2. 转换为 ES 文档格式，并更新 member_count
	for _, c := range dbCircles {
		newMemberCount := c.MemberCount + int(deltas[c.ID])
		if newMemberCount < 0 {
			newMemberCount = 0
		}

		circles = append(circles, elasticsearch.CircleDocument{
			ID:          c.ID,
			Name:        c.Name,
			AvatarURL:   c.AvatarURL,
			Description: c.Description,
			Hot:         c.Hot,
			CategoryID:  c.CategoryID,
			MemberCount: newMemberCount,
			PostCount:   c.PostCount,
			CreateTime:  c.CreateTime.Format(time.RFC3339),
			Status:      c.Status,
			Deleted:     c.Deleted,
			JoinType:    c.JoinType,
		})
	}

	if len(circles) == 0 {
		logger.Log.Warn("No circles found in database for ES update")
		return nil
	}

	// 3. 批量更新或插入到 ES
	if len(circles) < esBulkThreshold {
		// 小批量：逐条更新
		for _, doc := range circles {
			if err := updateOrInsertESDoc(&doc); err != nil {
				logger.Log.Warn(
					fmt.Sprintf("ES update failed: circle=%d err=%v", doc.ID, err),
				)
			} else {
				logger.Log.Info(fmt.Sprintf("ES document updated: circle=%d member_count=%d", doc.ID, doc.MemberCount))
			}
		}
	} else {
		// 中大批量：使用 bulk API
		if err := bulkUpdateOrInsertESDoc(circles); err != nil {
			logger.Log.Error("Failed to bulk update ES: " + err.Error())
			// 降级到逐条更新
			for _, doc := range circles {
				if err := updateOrInsertESDoc(&doc); err != nil {
					logger.Log.Warn(
						fmt.Sprintf("ES update failed: circle=%d err=%v", doc.ID, err),
					)
				}
			}
		} else {
			logger.Log.Info(fmt.Sprintf("Successfully bulk updated %d circle documents in ES", len(circles)))
		}
	}

	return nil
}

// updateOrInsertESDoc 更新或插入单个 ES 文档
func updateOrInsertESDoc(doc *elasticsearch.CircleDocument) error {
	// 先尝试更新
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	// 使用 upsert (doc_as_true) 语义：如果文档不存在则创建，存在则更新
	res, err := elasticsearch.Client.Index(
		conf.Config.Elasticsearch.Index,
		bytes.NewReader(docJSON),
		elasticsearch.Client.Index.WithDocumentID(fmt.Sprintf("%d", doc.ID)),
		elasticsearch.Client.Index.WithRefresh("false"),
		elasticsearch.Client.Index.WithOpType("index"), // index 操作会覆盖已存在的文档
	)
	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("elasticsearch error: %s", res.String())
	}

	return nil
}

// bulkUpdateOrInsertESDoc 批量更新或插入 ES 文档
func bulkUpdateOrInsertESDoc(docs []elasticsearch.CircleDocument) error {
	if len(docs) == 0 {
		return nil
	}

	indexName := conf.Config.Elasticsearch.Index

	// 构建 bulk 请求体
	var buf bytes.Buffer
	for _, doc := range docs {
		// 添加 index 操作
		meta := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
				"_id":    fmt.Sprintf("%d", doc.ID),
			},
		}
		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("failed to marshal meta: %w", err)
		}
		buf.Write(metaJSON)
		buf.WriteByte('\n')

		// 添加文档数据
		docJSON, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("failed to marshal document: %w", err)
		}
		buf.Write(docJSON)
		buf.WriteByte('\n')
	}

	// 执行 bulk 请求
	res, err := elasticsearch.Client.Bulk(
		bytes.NewReader(buf.Bytes()),
		elasticsearch.Client.Bulk.WithIndex(indexName),
		elasticsearch.Client.Bulk.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("failed to execute bulk request: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk request error: %s", res.String())
	}

	// 解析响应检查是否有错误
	var bulkResponse map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&bulkResponse); err != nil {
		return fmt.Errorf("failed to parse bulk response: %w", err)
	}

	// 检查是否有错误
	if warnings, ok := bulkResponse["warnings"]; ok && warnings != nil {
		logger.Log.Warn(fmt.Sprintf("ES bulk warnings: %v", warnings))
	}

	return nil
}

// Stop 停止聚合器
func (a *ESMemberCountAggregator) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.mu.Unlock()

	close(a.stopChan)
}

// StartESMemberCountConsumerWithRetry 启动 ES 消费者，带重试机制
func StartESMemberCountConsumerWithRetry() {
	maxAttempts := 10
	attempt := 0

	for {
		attempt++
		err := StartESMemberCountConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start ES member count consumer (attempt %d/%d): %s", attempt, maxAttempts, err.Error()))

			if attempt >= maxAttempts {
				logger.Log.Error("Max retry attempts reached for ES consumer, giving up")
				return
			}

			// 等待后重试
			waitTime := time.Duration(attempt) * 5 * time.Second
			logger.Log.Info(fmt.Sprintf("Retrying ES consumer in %v...", waitTime))
			time.Sleep(waitTime)
		} else {
			// 成功启动，退出重试循环
			logger.Log.Info("ES member count consumer started successfully, exiting retry loop")
			return
		}
	}
}
