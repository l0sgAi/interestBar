package rabbitmq

import (
	"encoding/json"
	"fmt"
	"interestBar/pkg/logger"
	es "interestBar/pkg/server/storage/elasticsearch"
	"time"

	conf "interestBar/pkg/conf"

	amqp "github.com/rabbitmq/amqp091-go"
)

// StartConsumer 启动消费者处理圈子同步消息
func StartConsumer() error {
	if channel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	// 设置 QoS，每次只接收一条消息
	err := channel.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	// 开始消费消息
	msgs, err := channel.Consume(
		CircleSyncQueue,
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

	logger.Log.Info("Circle sync consumer started")

	// 启动 goroutine 处理消息
	go func() {
		for d := range msgs {
			if err := processMessage(d); err != nil {
				logger.Log.Error("Failed to process message: " + err.Error())
				// 消息处理失败，重新入队（支持重试）
				d.Nack(false, true)
			} else {
				d.Ack(false)
			}
		}
	}()

	return nil
}

// processMessage 处理单条消息
func processMessage(d amqp.Delivery) error {
	var message CircleSyncMessage
	if err := json.Unmarshal(d.Body, &message); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	logger.Log.Info(fmt.Sprintf("Processing circle sync message: action=%s, circle_id=%d", message.Action, message.CircleID))

	// 根据操作类型执行相应的 ES 操作
	switch message.Action {
	case CircleSyncActionCreate:
		if err := es.IndexCircle(message.CircleID, message.Name, message.AvatarURL, message.Description, message.Hot, message.CategoryID, message.MemberCount, message.PostCount, message.CreateTime, message.Status, message.Deleted, message.JoinType); err != nil {
			return fmt.Errorf("failed to index circle: %w", err)
		}
	case CircleSyncActionUpdate:
		if err := es.UpdateCircle(message.CircleID, message.Name, message.AvatarURL, message.Description, message.Hot, message.CategoryID, message.MemberCount, message.PostCount, message.CreateTime, message.Status, message.Deleted, message.JoinType); err != nil {
			return fmt.Errorf("failed to update circle: %w", err)
		}
	case CircleSyncActionDelete:
		if err := es.DeleteCircle(message.CircleID); err != nil {
			return fmt.Errorf("failed to delete circle: %w", err)
		}
	default:
		logger.Log.Warn(fmt.Sprintf("Unknown action: %s", message.Action))
	}

	return nil
}

// StartConsumerWithRetry 启动消费者，带重试机制
func StartConsumerWithRetry() {
	maxAttempts := conf.Config.RabbitMQ.Retry.MaxAttempts
	attempt := 0

	for {
		attempt++
		err := StartConsumer()
		if err != nil {
			logger.Log.Error(fmt.Sprintf("Failed to start consumer (attempt %d/%d): %s", attempt, maxAttempts, err.Error()))

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
			logger.Log.Info("Consumer started successfully, exiting retry loop")
			return
		}
	}
}
