package infrastructure

import (
	"context"
	"errors"
	"time"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// replyLogRepoPG 基于 GORM 的 ReplyLogRepository 实现。
type replyLogRepoPG struct {
	db *gorm.DB
}

// NewReplyLogRepository 构造 ReplyLogRepository。
func NewReplyLogRepository(db *gorm.DB) domain.ReplyLogRepository {
	return &replyLogRepoPG{db: db}
}

func (r *replyLogRepoPG) Create(ctx context.Context, log *domain.ReplyLog) error {
	if err := r.db.WithContext(ctx).Create(log).Error; err != nil {
		// (agent_id, post_id) 唯一索引兜底并发防重：撞索引说明该帖已处理过。
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.ErrReplyAlreadyExists
		}
		return err
	}
	return nil
}

func (r *replyLogRepoPG) ExistsByAgentAndPost(ctx context.Context, agentID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("agent_id = ? AND post_id = ?", agentID, postID).
		Count(&count).Error
	return count > 0, err
}

func (r *replyLogRepoPG) CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("agent_id = ? AND create_time >= ?", agentID, since).
		Count(&count).Error
	return count, err
}

func (r *replyLogRepoPG) GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*domain.ReplyLog, error) {
	var log domain.ReplyLog
	err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("create_time DESC").
		First(&log).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &log, nil
}
