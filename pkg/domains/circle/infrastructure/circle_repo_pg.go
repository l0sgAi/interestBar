// Package infrastructure 提供 circle 领域基础设施层实现。
//
// 包括：
//   - circleRepoPG / memberRepoPG：基于 GORM 的 Repository 实现
//   - circleBaseCacheRedis / circleStatsCacheRedis / joinedCirclesCacheRedis：Redis 实现
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"interestBar/pkg/domains/circle/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// circleRepoPG 基于 GORM 的 CircleRepository 实现。
type circleRepoPG struct {
	db *gorm.DB
}

// NewCircleRepository 构造 CircleRepository。
func NewCircleRepository(db *gorm.DB) domain.CircleRepository {
	return &circleRepoPG{db: db}
}

func (r *circleRepoPG) GetByID(ctx context.Context, circleID uuid.UUID) (*domain.Circle, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", circleID, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrCircleNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *circleRepoPG) GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*domain.Circle, error) {
	if len(circleIDs) == 0 {
		return make(map[uuid.UUID]*domain.Circle), nil
	}
	var circles []domain.Circle
	err := r.db.WithContext(ctx).Where("id IN ? AND deleted = ?", circleIDs, 0).Find(&circles).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]*domain.Circle, len(circles))
	for i := range circles {
		m[circles[i].ID] = &circles[i]
	}
	return m, nil
}

func (r *circleRepoPG) ExistsByName(ctx context.Context, name string) (bool, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("name = ? AND deleted = ?", name, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *circleRepoPG) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var c domain.Circle
	err := r.db.WithContext(ctx).Where("slug = ? AND deleted = ?", slug, 0).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Create 创建圈子并自动将创建者设为圈主（事务），与旧 model.CreateCircle 一致。
func (r *circleRepoPG) Create(ctx context.Context, circle *domain.Circle) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if circle.ID == uuid.Nil {
			circle.ID = sharedomain.NewID()
		}
		if err := tx.Create(circle).Error; err != nil {
			return err
		}
		member := domain.CircleMember{
			CircleID: circle.ID,
			UserID:   circle.CreatorID,
			Role:     domain.MemberRoleOwner,
			Status:   domain.MemberStatusNormal,
			IsTop:    0,
			IsDisturb: 0,
		}
		if member.ID == uuid.Nil {
			member.ID = sharedomain.NewID()
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		return nil
	})
}

// memberRepoPG 基于 GORM 的 MemberRepository 实现。
type memberRepoPG struct {
	db *gorm.DB
}

// NewMemberRepository 构造 MemberRepository。
func NewMemberRepository(db *gorm.DB) domain.MemberRepository {
	return &memberRepoPG{db: db}
}

func (r *memberRepoPG) GetMember(ctx context.Context, circleID, userID uuid.UUID) (*domain.CircleMember, error) {
	var m domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrMemberNotFound
		}
		return nil, err
	}
	return &m, nil
}

// ListJoinedWithScore 列出用户 normal 成员的 (circleID, 加入时间ms)，按加入时间倒序。
// limit=0 表示不限制。用于 JoinedCirclesCache 重建。
func (r *memberRepoPG) ListJoinedWithScore(ctx context.Context, userID uuid.UUID, limit int) ([]domain.JoinedMember, error) {
	var rows []struct {
		CircleID   uuid.UUID `gorm:"column:circle_id"`
		CreateTime time.Time `gorm:"column:create_time"`
	}
	q := r.db.WithContext(ctx).Model(&domain.CircleMember{}).
		Where("user_id = ? AND status = ?", userID, domain.MemberStatusNormal).
		Order("create_time DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Select("circle_id, create_time").Find(&rows).Error; err != nil {
		return nil, err
	}
	members := make([]domain.JoinedMember, len(rows))
	for i, row := range rows {
		members[i] = domain.JoinedMember{
			CircleID: row.CircleID,
			ScoreMs:  row.CreateTime.UnixMilli(),
		}
	}
	return members, nil
}

// JoinCircle 用户加入圈子，与旧 model.JoinCircle 状态机完全一致。
func (r *memberRepoPG) JoinCircle(ctx context.Context, circleID, userID uuid.UUID, joinType int16) (*domain.CircleMember, error) {
	var existing domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&existing).Error
	if err == nil {
		// 已存在成员记录
		if existing.Status == domain.MemberStatusBanned {
			return nil, fmt.Errorf("user is banned from this circle")
		}
		if existing.Status == domain.MemberStatusNormal {
			return nil, fmt.Errorf("user is already a member of this circle")
		}
		if existing.Status == domain.MemberStatusPending || existing.Status == domain.MemberStatusLeft {
			existing.Status = domain.MemberStatusNormal
			if e := r.db.WithContext(ctx).Save(&existing).Error; e != nil {
				return nil, e
			}
			return &existing, nil
		}
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var status int16 = domain.MemberStatusNormal
	if joinType == domain.CircleJoinTypeApproval {
		status = domain.MemberStatusPending
	} else if joinType == domain.CircleJoinTypePrivate {
		return nil, fmt.Errorf("this circle is private and requires invitation")
	}

	member := domain.CircleMember{
		CircleID: circleID,
		UserID:   userID,
		Role:     domain.MemberRoleMember,
		Status:   status,
		IsTop:    0,
		IsDisturb: 0,
	}
	if member.ID == uuid.Nil {
		member.ID = sharedomain.NewID()
	}
	if err := r.db.WithContext(ctx).Create(&member).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

// LeaveCircle 与旧 model.LeaveCircle 一致。
func (r *memberRepoPG) LeaveCircle(ctx context.Context, circleID, userID uuid.UUID) error {
	var member domain.CircleMember
	err := r.db.WithContext(ctx).
		Where("circle_id = ? AND user_id = ?", circleID, userID).
		First(&member).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("user is not a member of this circle")
		}
		return err
	}
	if member.Role == domain.MemberRoleOwner {
		return fmt.Errorf("circle owner cannot leave the circle")
	}
	if member.Status != domain.MemberStatusNormal {
		return fmt.Errorf("member status is not normal, cannot leave")
	}
	return r.db.WithContext(ctx).Model(&member).Update("status", domain.MemberStatusLeft).Error
}
