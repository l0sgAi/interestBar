package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

var conn *amqp.Connection
var channel *amqp.Channel

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

	// 声明成员计数交换机
	err = channel.ExchangeDeclare(
		CircleMemberCountExchange,
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
		return fmt.Errorf("failed to declare member count exchange: %w", err)
	}

	// 声明成员计数队列
	memberQ, err := channel.QueueDeclare(
		CircleMemberCountQueue,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to declare member count queue: %w", err)
	}

	// 绑定成员计数队列到交换机
	err = channel.QueueBind(
		memberQ.Name,
		CircleMemberCountRoutingKey,
		CircleMemberCountExchange,
		false,
		nil,
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to bind member count queue: %w", err)
	}

	logger.Log.Info("RabbitMQ initialized successfully")
	return nil
}

// PublishJoinMsg 发布圈子成员加入/退出消息
func PublishJoinMsg(circleID uuid.UUID, isJoin int64) error {
	if channel == nil {
		return fmt.Errorf("RabbitMQ channel is not initialized")
	}

	message := JoinMsg{
		CircleID: circleID,
		IsJoin:   isJoin,
	}

	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal join message: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = channel.PublishWithContext(ctx,
		CircleMemberCountExchange,
		CircleMemberCountRoutingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // 持久化消息
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish join message: %w", err)
	}

	action := "join"
	if isJoin == -1 {
		action = "leave"
	}
	logger.Log.Info(fmt.Sprintf("Published member count message: action=%s, circle_id=%s", action, circleID.String()))
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
