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

var (
	dialer *kafka.Dialer
	writer *kafka.Writer
)

// InitRedpandaProducer 初始化Redpanda Producer
func InitRedpandaProducer() error {
	// 创建自定义Dialer，设置更长的超时时间
	dialer = &kafka.Dialer{
		Timeout:   10 * time.Second, // 连接超时
		DualStack: true,             // 启用IPv4/IPv6双栈
	}

	writer = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.Topic,
		AllowAutoTopicCreation: true, // 自动创建topic
		Balancer:               &kafka.LeastBytes{}, // 使用LeastBytes均衡器
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne, // 只需要leader确认
		Compression:            kafka.Snappy,     // 使用Snappy压缩
		Async:                  true,             // 异步发送提升性能
		MaxAttempts:            5,                // 增加重试次数
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Redpanda producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.Topic))

	// 测试连接 - 尝试连接到Redpanda
	conn, err := dialer.DialContext(context.Background(), "tcp", conf.Config.Redpanda.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to redpanda: %w", err)
	}
	defer conn.Close()

	// 设置连接超时
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	// 检查连接是否正常
	_, err = conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get redpanda controller: %w", err)
	}

	logger.Log.Info("Redpanda connection test successful")
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
