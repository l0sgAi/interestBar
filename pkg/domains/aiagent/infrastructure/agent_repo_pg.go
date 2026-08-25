// Package infrastructure 提供 aiagent 领域基础设施层实现。
package infrastructure

import (
	"context"
	"errors"

	"interestBar/pkg/domains/aiagent/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// agentRepoPG 基于 GORM 的 AgentRepository 实现。
type agentRepoPG struct {
	db *gorm.DB
}

// NewAgentRepository 构造 AgentRepository。
func NewAgentRepository(db *gorm.DB) domain.AgentRepository {
	return &agentRepoPG{db: db}
}

func (r *agentRepoPG) Create(ctx context.Context, agent *domain.AiAgent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepoPG) GetByID(ctx context.Context, agentID uuid.UUID) (*domain.AiAgent, error) {
	var a domain.AiAgent
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", agentID, 0).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrAgentNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *agentRepoPG) ExistsByName(ctx context.Context, name string, excludeID uuid.UUID) (bool, error) {
	var a domain.AiAgent
	query := r.db.WithContext(ctx).Where("name = ? AND deleted = ?", name, 0)
	if excludeID != uuid.Nil {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *agentRepoPG) ListByOffset(ctx context.Context, offset, limit int) ([]domain.AiAgent, int64, error) {
	var (
		agents []domain.AiAgent
		total  int64
	)
	if err := r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("deleted = ?", 0).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).
		Where("deleted = ?", 0).
		Order("create_time DESC").
		Offset(offset).Limit(limit).
		Find(&agents).Error
	if err != nil {
		return nil, 0, err
	}
	return agents, total, nil
}

func (r *agentRepoPG) UpdateFields(ctx context.Context, agentID uuid.UUID, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("id = ? AND deleted = ?", agentID, 0).
		Updates(fields).Error
}

func (r *agentRepoPG) SoftDelete(ctx context.Context, agentID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.AiAgent{}).
		Where("id = ? AND deleted = ?", agentID, 0).
		Updates(map[string]interface{}{
			"deleted": 1,
			"status":  domain.AgentStatusDisabled,
		}).Error
}
