package redpanda

import (
	"context"
	"encoding/json"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

var (
	dialer          *kafka.Dialer
	writer          *kafka.Writer
	postWriter      *kafka.Writer
	likeEventWriter *kafka.Writer
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
		AllowAutoTopicCreation: true,                // 自动创建topic
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
func PublishCircleMemberCount(circleID uuid.UUID, value int64) error {
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
func PublishCirclePostCount(circleID uuid.UUID) error {
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
		Key:   []byte(message.CircleID.String()), // 使用circle_id作为key，保证同一圈子的消息有序
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
	logger.Log.Debug(fmt.Sprintf("Published %s message: type=%s, circle_id=%s, value=%d",
		action, message.Type, message.CircleID.String(), message.Value))

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

// ==================== 帖子统计消息 ====================

// InitPostStatsProducer 初始化帖子统计Producer
func InitPostStatsProducer() error {
	postWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.PostTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Post stats producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.PostTopic))

	return nil
}

// PublishPostViewCount 发布帖子浏览量变化消息
func PublishPostViewCount(postID uuid.UUID) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostView,
		PostID: postID,
		Value:  1,
	})
}

// PublishPostCommentCount 发布帖子评论数变化消息
func PublishPostCommentCount(postID uuid.UUID, value int64) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostComment,
		PostID: postID,
		Value:  value,
	})
}

// PublishPostLikeCount 发布帖子点赞数变化消息
func PublishPostLikeCount(postID uuid.UUID, value int64) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostLike,
		PostID: postID,
		Value:  value,
	})
}

// PublishPostCollectCount 发布帖子收藏数变化消息
func PublishPostCollectCount(postID uuid.UUID, value int64) error {
	if postWriter == nil {
		return fmt.Errorf("post stats writer is not initialized")
	}
	return publishPostMessage(PostStatisticsMessage{
		Type:   StatisticsTypePostCollect,
		PostID: postID,
		Value:  value,
	})
}

// publishPostMessage 发送帖子统计消息到Redpanda
func publishPostMessage(message PostStatisticsMessage) error {
	value, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal post stats message: %w", err)
	}

	kafkaMsg := kafka.Message{
		Key:   []byte(message.PostID.String()),
		Value: value,
	}

	err = postWriter.WriteMessages(context.Background(), kafkaMsg)
	if err != nil {
		return fmt.Errorf("failed to write post stats message: %w", err)
	}

	logger.Log.Debug(fmt.Sprintf("Published post stats message: type=%s, post_id=%s, value=%d",
		message.Type, message.PostID.String(), message.Value))

	return nil
}

// ClosePostStatsProducer 关闭帖子统计Producer
func ClosePostStatsProducer() error {
	if postWriter != nil {
		if err := postWriter.Close(); err != nil {
			logger.Log.Error("Failed to close post stats writer: " + err.Error())
			return err
		}
		logger.Log.Info("Post stats producer closed")
	}
	return nil
}

// ==================== 点赞事件消息 ====================

// InitLikeEventProducer 初始化点赞事件Producer
func InitLikeEventProducer() error {
	likeEventWriter = &kafka.Writer{
		Addr:                   kafka.TCP(conf.Config.Redpanda.Brokers...),
		Topic:                  conf.Config.Redpanda.LikeEventTopic,
		AllowAutoTopicCreation: true,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		RequiredAcks:           kafka.RequireOne,
		Compression:            kafka.Snappy,
		Async:                  true,
		MaxAttempts:            5,
		ReadTimeout:            10 * time.Second,
		WriteTimeout:           10 * time.Second,
	}

	logger.Log.Info(fmt.Sprintf("Like event producer initialized: brokers=%v, topic=%s",
		conf.Config.Redpanda.Brokers, conf.Config.Redpanda.LikeEventTopic))
	return nil
}

// PublishCommentLikeEvent 发布评论点赞事件消息
func PublishCommentLikeEvent(userID, commentID, postID uuid.UUID, amount int64) error {
	if likeEventWriter == nil {
		return fmt.Errorf("like event writer is not initialized")
	}
	return publishLikeEvent(LikeEventMessage{
		Type:     "comment_like",
		UserID:   userID,
		TargetID: commentID,
		PostID:   postID,
		Amount:   amount,
	})
}

// PublishPostLikeEvent 发布帖子点赞事件消息
func PublishPostLikeEvent(userID, postID uuid.UUID, amount int64) error {
	if likeEventWriter == nil {
		return fmt.Errorf("like event writer is not initialized")
	}
	return publishLikeEvent(LikeEventMessage{
		Type:     "post_like",
		UserID:   userID,
		TargetID: postID,
		Amount:   amount,
	})
}

func publishLikeEvent(msg LikeEventMessage) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal like event message: %w", err)
	}
	kafkaMsg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%s:%s", msg.UserID.String(), msg.TargetID.String())),
		Value: value,
	}
	if err := likeEventWriter.WriteMessages(context.Background(), kafkaMsg); err != nil {
		return fmt.Errorf("failed to write like event message: %w", err)
	}
	logger.Log.Debug(fmt.Sprintf("Published like event: type=%s, user=%s, target=%s, amount=%d",
		msg.Type, msg.UserID.String(), msg.TargetID.String(), msg.Amount))
	return nil
}

// CloseLikeEventProducer 关闭点赞事件Producer
func CloseLikeEventProducer() error {
	if likeEventWriter != nil {
		if err := likeEventWriter.Close(); err != nil {
			logger.Log.Error("Failed to close like event writer: " + err.Error())
			return err
		}
		logger.Log.Info("Like event producer closed")
	}
	return nil
}
