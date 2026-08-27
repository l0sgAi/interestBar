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
	return r.db.WithContext(ctx).Create(log).Error
}

// CountSinceByAgent 限频口径排除 status=2/3（分类器未产出回复的判定/运维事件，不占配额）。
func (r *replyLogRepoPG) CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.ReplyLog{}).
		Where("agent_id = ? AND create_time >= ? AND status NOT IN ?", agentID, since, domain.RateLimitExcludedStatuses).
		Count(&count).Error
	return count, err
}

// GetLastByAgent 限频口径排除 status=2/3（同上，最小间隔只相对真实回复/失败尝试计算）。
func (r *replyLogRepoPG) GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*domain.ReplyLog, error) {
	var log domain.ReplyLog
	err := r.db.WithContext(ctx).
		Where("agent_id = ? AND status NOT IN ?", agentID, domain.RateLimitExcludedStatuses).
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
