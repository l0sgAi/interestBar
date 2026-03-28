package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	"github.com/segmentio/kafka-go"
)

var writer *kafka.Writer

// InitRedpandaProducer 初始化Redpanda Producer
func InitRedpandaProducer() error {
	writer = &kafka.Writer{
		Addr:         kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:        conf.Config.Redpanda.Topic,
		Balancer:     &kafka.LeastBytes{}, // 使用LeastBytes均衡器
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne, // 只需要leader确认
		Compression:  kafka.Snappy,     // 使用Snappy压缩
		Async:        true,             // 异步发送提升性能
	}

	logger.Log.Info(fmt.Sprintf("Redpanda producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.Topic))

	return nil
}

// PublishCircleMemberCount 发布圈子成员计数变化消息
func PublishCircleMemberCount(circleID int64, value int64) error {
	if writer == nil {
		return fmt.Errorf("redpanda writer is not initialized")
	}

	message := CircleStatisticsMessage{
		Type:     StatisticsTypeCircleCount,
		CircleID: circleID,
		Value:    value,
	}

	return publishMessage(message)
}

// PublishCirclePostCount 发布圈子帖子计数变化消息
func PublishCirclePostCount(circleID int64) error {
	if writer == nil {
		return fmt.Errorf("redpanda writer is not initialized")
	}

	message := CircleStatisticsMessage{
		Type:     StatisticsTypePostCount,
		CircleID: circleID,
		Value:    1,
	}

	return publishMessage(message)
}

// publishMessage 发送消息到Redpanda
func publishMessage(message CircleStatisticsMessage) error {
	value, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%d", message.CircleID)), // 使用circle_id作为key，保证同一圈子的消息有序
		Value: value,
	}

	// 异步发送
	err = writer.WriteMessages(context.Background(), kafkaMsg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	action := "increment"
	if message.Value < 0 {
		action = "decrement"
	}
	logger.Log.Debug(fmt.Sprintf("Published %s message: type=%s, circle_id=%d, value=%d",
		action, message.Type, message.CircleID, message.Value))

	return nil
}

// CloseRedpandaProducer 关闭Redpanda Producer
func CloseRedpandaProducer() error {
	if writer != nil {
		if err := writer.Close(); err != nil {
			logger.Log.Error("Failed to close redpanda writer: " + err.Error())
			return err
		}
		logger.Log.Info("Redpanda producer closed")
	}
	return nil
}
