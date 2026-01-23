package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var conn *amqp.Connection
var channel *amqp.Channel

// CircleSyncMessage 圈子同步消息结构
type CircleSyncMessage struct {
	Action      string `json:"action"` // create, update, delete
	CircleID    int64  `json:"circle_id"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Description string `json:"description"`
	Hot         int    `json:"hot"`
	CategoryID  int    `json:"category_id"`
	MemberCount int    `json:"member_count"`
	PostCount   int    `json:"post_count"`
	CreateTime  string `json:"create_time"` // ISO 8601格式
	Status      int16  `json:"status"`
	Deleted     int16  `json:"deleted"`
	JoinType    int16  `json:"join_type"`
}

// InitRabbitMQ 初始化 RabbitMQ 连接
func InitRabbitMQ() error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d%s",
		conf.Config.RabbitMQ.Username,
		conf.Config.RabbitMQ.Password,
		conf.Config.RabbitMQ.Host,
		conf.Config.RabbitMQ.Port,
		conf.Config.RabbitMQ.VHost,
	)

	var err error
	conn, err = amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	channel, err = conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open RabbitMQ channel: %w", err)
	}

	// 声明交换机
	err = channel.ExchangeDeclare(
		CircleSyncExchange,
		"direct", // 交换机类型
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	// 声明队列
	q, err := channel.QueueDeclare(
		CircleSyncQueue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// 绑定队列到交换机
	err = channel.QueueBind(
		q.Name,
		CircleSyncRoutingKey,
		CircleSyncExchange,
		false,
		nil,
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	logger.Log.Info("RabbitMQ initialized successfully")
	return nil
}

// PublishCircleSync 发布圈子同步消息
func PublishCircleSync(action string, circleID int64, name string, avatarURL string, description string, hot int, categoryID int, memberCount int, postCount int, createTime string, status int16, deleted int16, joinType int16) error {
	if channel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	message := CircleSyncMessage{
		Action:      action,
		CircleID:    circleID,
		Name:        name,
		AvatarURL:   avatarURL,
		Description: description,
		Hot:         hot,
		CategoryID:  categoryID,
		MemberCount: memberCount,
		PostCount:   postCount,
		CreateTime:  createTime,
		Status:      status,
		Deleted:     deleted,
		JoinType:    joinType,
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = channel.PublishWithContext(ctx,
		CircleSyncExchange,
		CircleSyncRoutingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // 持久化消息
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	logger.Log.Info(fmt.Sprintf("Published circle sync message: action=%s, circle_id=%d", action, circleID))
	return nil
}

// CloseRabbitMQ 关闭 RabbitMQ 连接
func CloseRabbitMQ() error {
	var err error
	if channel != nil {
		if e := channel.Close(); e != nil {
			logger.Log.Error("Failed to close RabbitMQ channel: " + e.Error())
			err = e
		}
	}
	if conn != nil {
		if e := conn.Close(); e != nil {
			logger.Log.Error("Failed to close RabbitMQ connection: " + e.Error())
			if err == nil {
				err = e
			}
		}
	}
	return err
}
